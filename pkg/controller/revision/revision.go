/*
Copyright 2018 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package revision

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	buildv1alpha1 "github.com/elafros/build/pkg/apis/build/v1alpha1"
	buildinformers "github.com/elafros/build/pkg/client/informers/externalversions"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	listers "github.com/elafros/elafros/pkg/client/listers/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"
)

const (
	elaContainerName string = "ela-container"
	elaPortName      string = "ela-port"
	elaPort                 = 8080

	fluentdContainerName string = "fluentd-proxy"
	queueContainerName   string = "queue-proxy"
	// queueSidecarName set by -queueSidecarName flag
	queueHttpPortName string = "queue-http-port"

	requestQueueContainerName string = "request-queue"

	controllerAgentName = "revision-controller"
	autoscalerPort      = 8080

	serviceTimeoutDuration       = 5 * time.Minute
	sidecarIstioInjectAnnotation = "sidecar.istio.io/inject"
	// TODO (arvtiwar): this should be a config option.
	progressDeadlineSeconds int32 = 120
)

var (
	elaPodReplicaCount   = int32(1)
	elaPodMaxUnavailable = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
	elaPodMaxSurge       = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
)

// Helper to make sure we log error messages returned by Reconcile().
func printErr(err error) error {
	if err != nil {
		log.Printf("Logging error: %s", err)
	}
	return err
}

type resolver interface {
	Resolve(*appsv1.Deployment) error
}

// Controller implements the controller for Revision resources.
// +controller:group=ela,version=v1alpha1,kind=Revision,resource=revisions
type Controller struct {
	base *controller.ControllerBase

	// lister indexes properties about Revision
	lister listers.RevisionLister
	synced cache.InformerSynced

	buildtracker *buildTracker

	resolver resolver

	// don't start the workers until endpoints cache have been synced
	endpointsSynced cache.InformerSynced

	// enableVarLogCollection dedicates whether to set up a fluentd sidecar to
	// collect logs under /var/log/.
	enableVarLogCollection bool

	// controllerConfig includes the configurations for the controller
	controllerConfig *ControllerConfig
}

// ControllerConfig includes the configurations for the controller.
type ControllerConfig struct {
	// Autoscale part

	// see (elaconfig.yaml)
	AutoscaleConcurrencyQuantumOfTime *k8sflag.DurationFlag
	AutoscaleEnableSingleConcurrency  *k8sflag.BoolFlag

	// AutoscalerImage is the name of the image used for the autoscaler pod.
	AutoscalerImage string

	// QueueSidecarImage is the name of the image used for the queue sidecar
	// injected into the revision pod
	QueueSidecarImage string

	// logging part

	// EnableVarLogCollection dedicates whether to set up a fluentd sidecar to
	// collect logs under /var/log/.
	EnableVarLogCollection bool

	// FluentdSidecarImage is the name of the image used for the fluentd sidecar
	// injected into the revision pod. It is used only when enableVarLogCollection
	// is true.
	FluentdSidecarImage string

	// LoggingURLTemplate is a string containing the logging url template where
	// the variable REVISION_UID will be replaced with the created revision's UID.
	LoggingURLTemplate string
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// queue - message queue for handling new events.  unique to this controller.
func NewController(
	base *controller.ControllerBase,
	buildInformerFactory buildinformers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig *ControllerConfig) controller.Interface {

	// obtain references to a shared index informer for the Revision and Endpoint type.
	informer := base.ElaInformerFactory.Elafros().V1alpha1().Revisions()
	endpointsInformer := base.KubeInformerFactory.Core().V1().Endpoints()
	deploymentInformer := base.KubeInformerFactory.Apps().V1().Deployments()

	base.Init(controllerAgentName, "Revisions", informer.Informer())
	controller := &Controller{
		base:             base,
		lister:           informer.Lister(),
		synced:           informer.Informer().HasSynced,
		endpointsSynced:  endpointsInformer.Informer().HasSynced,
		buildtracker:     &buildTracker{builds: map[key]set{}},
		resolver:         &digestResolver{client: base.KubeClientSet, transport: http.DefaultTransport},
		controllerConfig: controllerConfig,
	}

	glog.Info("Setting up event handlers")
	// Obtain a reference to a shared index informer for the Build type.
	buildInformer := buildInformerFactory.Build().V1alpha1().Builds()
	buildInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.addBuildEvent,
		UpdateFunc: controller.updateBuildEvent,
	})

	endpointsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.addEndpointsEvent,
		UpdateFunc: controller.updateEndpointsEvent,
	})

	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.addDeploymentProgressEvent,
		UpdateFunc: controller.updateDeploymentProgressEvent,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.base.Run(threadiness, stopCh, []cache.InformerSynced{c.synced, c.endpointsSynced},
		c.syncHandler, "Revision")
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Revision resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	log.Printf("Running reconcile Revision for %q:%q\n", namespace, name)

	// Get the Revision resource with this namespace/name
	rev, err := c.lister.Revisions(namespace).Get(name)
	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("revision %q in work queue no longer exists", key))
			return nil
		}
		return err
	}
	// Don't modify the informer's copy.
	rev = rev.DeepCopy()

	if err := c.updateRevisionLoggingURL(rev); err != nil {
		glog.Errorf("Error updating the revisions logging url: %s", err)
		return err
	}

	if rev.Spec.BuildName != "" {
		if done, failed := isBuildDone(rev); !done {
			if alreadyTracked := c.buildtracker.Track(rev); !alreadyTracked {
				if err := c.markRevisionBuilding(rev); err != nil {
					glog.Errorf("Error recording the BuildSucceeded=Unknown condition: %s", err)
					return err
				}
			}
			return nil
		} else {
			// The Build's complete, so stop tracking it.
			c.buildtracker.Untrack(rev)
			if failed {
				return nil
			}
			// If the build didn't fail, proceed to creating K8s resources.
		}
	}

	ns, err := controller.GetOrCreateRevisionNamespace(namespace, c.base.KubeClientSet)
	if err != nil {
		log.Printf("Failed to create namespace: %s", err)
		panic("Failed to create namespace")
	}
	log.Printf("Namespace %q validated to exist, moving on", ns)

	return c.reconcileWithImage(rev, namespace)
}

// reconcileWithImage handles enqueued messages that have an image.
func (c *Controller) reconcileWithImage(rev *v1alpha1.Revision, ns string) error {
	return printErr(c.reconcileOnceBuilt(rev, ns))
}

func (c *Controller) updateRevisionLoggingURL(rev *v1alpha1.Revision) error {
	logURLTmpl := c.controllerConfig.LoggingURLTemplate
	if logURLTmpl == "" {
		return nil
	}

	url := strings.Replace(logURLTmpl, "${REVISION_UID}", string(rev.UID), -1)

	if rev.Status.LogURL == url {
		return nil
	}
	rev.Status.LogURL = url
	_, err := c.updateStatus(rev)
	return err
}

// Checks whether the Revision knows whether the build is done.
// TODO(mattmoor): Use a method on the Build type.
func isBuildDone(rev *v1alpha1.Revision) (done, failed bool) {
	if rev.Spec.BuildName == "" {
		return true, false
	}
	for _, cond := range rev.Status.Conditions {
		if cond.Type != v1alpha1.RevisionConditionBuildSucceeded {
			continue
		}
		switch cond.Status {
		case corev1.ConditionTrue:
			return true, false
		case corev1.ConditionFalse:
			return true, true
		case corev1.ConditionUnknown:
			return false, false
		}
	}
	return false, false
}

func (c *Controller) markRevisionReady(rev *v1alpha1.Revision) error {
	glog.Infof("Marking Revision %q ready", rev.Name)
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
			Reason: "ServiceReady",
		})
	_, err := c.updateStatus(rev)
	return err
}

func (c *Controller) markRevisionFailed(rev *v1alpha1.Revision) error {
	glog.Infof("Marking Revision %q failed", rev.Name)
	reason, message := "ServiceTimeout", "Timed out waiting for a service endpoint to become ready"
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:    v1alpha1.RevisionConditionResourcesAvailable,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:    v1alpha1.RevisionConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	_, err := c.updateStatus(rev)
	return err
}

func (c *Controller) markRevisionBuilding(rev *v1alpha1.Revision) error {
	reason := "Building"
	glog.Infof("Marking Revision %q %s", rev.Name, reason)
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionBuildSucceeded,
			Status: corev1.ConditionUnknown,
			Reason: reason,
		})
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionUnknown,
			Reason: reason,
		})
	// Let this trigger a reconciliation loop.
	_, err := c.updateStatus(rev)
	return err
}

func (c *Controller) markBuildComplete(rev *v1alpha1.Revision, bc *buildv1alpha1.BuildCondition) error {
	switch bc.Type {
	case buildv1alpha1.BuildComplete:
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:   v1alpha1.RevisionConditionBuildSucceeded,
				Status: corev1.ConditionTrue,
			})
		c.base.Recorder.Event(rev, corev1.EventTypeNormal, "BuildComplete", bc.Message)
	case buildv1alpha1.BuildFailed, buildv1alpha1.BuildInvalid:
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionBuildSucceeded,
				Status:  corev1.ConditionFalse,
				Reason:  bc.Reason,
				Message: bc.Message,
			})
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  bc.Reason,
				Message: bc.Message,
			})
		c.base.Recorder.Event(rev, corev1.EventTypeWarning, "BuildFailed", bc.Message)
	}
	// This will trigger a reconciliation that will cause us to stop tracking the build.
	_, err := c.updateStatus(rev)
	return err
}

func getBuildDoneCondition(build *buildv1alpha1.Build) *buildv1alpha1.BuildCondition {
	for _, cond := range build.Status.Conditions {
		if cond.Status != corev1.ConditionTrue {
			continue
		}
		switch cond.Type {
		case buildv1alpha1.BuildComplete, buildv1alpha1.BuildFailed, buildv1alpha1.BuildInvalid:
			return &cond
		}
	}
	return nil
}

func getIsServiceReady(e *corev1.Endpoints) bool {
	for _, es := range e.Subsets {
		if len(es.Addresses) > 0 {
			return true
		}
	}
	return false
}

func getRevisionLastTransitionTime(r *v1alpha1.Revision) time.Time {
	condCount := len(r.Status.Conditions)
	if condCount == 0 {
		return r.CreationTimestamp.Time
	}
	return r.Status.Conditions[condCount-1].LastTransitionTime.Time
}

func getDeploymentProgressCondition(deployment *appsv1.Deployment) *appsv1.DeploymentCondition {

	//as per https://kubernetes.io/docs/concepts/workloads/controllers/deployment
	for _, cond := range deployment.Status.Conditions {
		// Look for Deployment with status False
		if cond.Status != corev1.ConditionFalse {
			continue
		}
		// with Type Progressing and Reason Timeout
		// TODO (arvtiwar): hard coding "ProgressDeadlineExceeded" to avoid import kubernetes/kubernetes
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			return &cond
		}
	}
	return nil
}

func (c *Controller) addBuildEvent(obj interface{}) {
	build := obj.(*buildv1alpha1.Build)

	cond := getBuildDoneCondition(build)
	if cond == nil {
		// The build isn't done, so ignore this event.
		return
	}

	// For each of the revisions watching this build, mark their build phase as complete.
	for k := range c.buildtracker.GetTrackers(build) {
		// Look up the revision to mark complete.
		namespace, name := splitKey(k)
		rev, err := c.lister.Revisions(namespace).Get(name)
		if err != nil {
			glog.Errorf("Error fetching revision %q/%q upon build completion: %v", namespace, name, err)
		}
		if err := c.markBuildComplete(rev, cond); err != nil {
			glog.Errorf("Error marking build completion for %q/%q: %v", namespace, name, err)
		}
	}

	return
}

func (c *Controller) updateBuildEvent(old, new interface{}) {
	c.addBuildEvent(new)
}

func (c *Controller) addDeploymentProgressEvent(obj interface{}) {
	deployment := obj.(*appsv1.Deployment)
	cond := getDeploymentProgressCondition(deployment)

	if cond == nil {
		return
	}

	//Get the handle of Revision in context
	revName := deployment.Name
	namespace := deployment.Namespace

	rev, err := c.lister.Revisions(namespace).Get(revName)
	if err != nil {
		glog.Errorf("Error fetching revision '%s/%s': %v", namespace, revName, err)
		return
	}
	//Set the revision condition reason to ProgressDeadlineExceeded
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:    v1alpha1.RevisionConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  "ProgressDeadlineExceeded",
			Message: fmt.Sprintf("Unable to create pods for more than %d seconds.", progressDeadlineSeconds),
		})

	glog.Infof("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		glog.Errorf("Error recording revision completion: %s", err)
		return
	}
	c.base.Recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded", "Revision %s not ready due to Deployment timeout", revName)
	return
}

func (c *Controller) updateDeploymentProgressEvent(old, new interface{}) {
	c.addDeploymentProgressEvent(new)
}

func (c *Controller) addEndpointsEvent(obj interface{}) {
	endpoint := obj.(*corev1.Endpoints)
	eName := endpoint.Name
	namespace := endpoint.Namespace
	// Lookup and see if this endpoints corresponds to a service that
	// we own and hence the Revision that created this service.
	revName := lookupServiceOwner(endpoint)
	if revName == "" {
		return
	}

	rev, err := c.lister.Revisions(namespace).Get(revName)
	if err != nil {
		glog.Errorf("Error fetching revision '%s/%s': %v", namespace, revName, err)
		return
	}

	// Check to see if endpoint is the service endpoint
	if eName != controller.GetElaK8SServiceNameForRevision(rev) {
		return
	}

	// Check to see if the revision has already been marked as ready or failed
	// and if it is, then there's no need to do anything to it.
	if rev.Status.IsReady() || rev.Status.IsFailed() {
		return
	}

	// Don't modify the informer's copy.
	rev = rev.DeepCopy()

	if getIsServiceReady(endpoint) {
		glog.Infof("Endpoint %q is ready", eName)
		if err := c.markRevisionReady(rev); err != nil {
			glog.Errorf("Error marking revision ready for '%s/%s': %v", namespace, revName, err)
			return
		}
		c.base.Recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon endpoint %q becoming ready", endpoint.Name)
		return
	}

	revisionAge := time.Now().Sub(getRevisionLastTransitionTime(rev))
	if revisionAge < serviceTimeoutDuration {
		return
	}

	if err := c.markRevisionFailed(rev); err != nil {
		glog.Errorf("Error marking revision failed for '%s/%s': %v", namespace, revName, err)
		return
	}
	c.base.Recorder.Eventf(rev, corev1.EventTypeWarning, "RevisionFailed", "Revision did not become ready due to endpoint %q", endpoint.Name)
	return
}

func (c *Controller) updateEndpointsEvent(old, new interface{}) {
	c.addEndpointsEvent(new)
}

// reconcileOnceBuilt handles enqueued messages that have an image.
func (c *Controller) reconcileOnceBuilt(rev *v1alpha1.Revision, ns string) error {
	accessor, err := meta.Accessor(rev)
	if err != nil {
		log.Printf("Failed to get metadata: %s", err)
		panic("Failed to get metadata")
	}

	deletionTimestamp := accessor.GetDeletionTimestamp()
	log.Printf("Check the deletionTimestamp: %s\n", deletionTimestamp)

	elaNS := controller.GetElaNamespaceName(rev.Namespace)

	if deletionTimestamp == nil && rev.Spec.ServingState == v1alpha1.RevisionServingStateActive {
		log.Printf("Creating or reconciling resources for %s\n", rev.Name)
		return c.createK8SResources(rev, elaNS)
	}
	return c.deleteK8SResources(rev, elaNS)
}

func (c *Controller) deleteK8SResources(rev *v1alpha1.Revision, ns string) error {
	log.Printf("Deleting the resources for %s\n", rev.Name)
	err := c.deleteDeployment(rev, ns)
	if err != nil {
		log.Printf("Failed to delete a deployment: %s", err)
	}
	log.Printf("Deleted deployment")

	err = c.deleteAutoscalerDeployment(rev)
	if err != nil {
		log.Printf("Failed to delete autoscaler Deployment: %s", err)
	}
	log.Printf("Deleted autoscaler Deployment")

	err = c.deleteAutoscalerService(rev)
	if err != nil {
		log.Printf("Failed to delete autoscaler Service: %s", err)
	}
	log.Printf("Deleted autoscaler Service")

	err = c.deleteService(rev, ns)
	if err != nil {
		log.Printf("Failed to delete k8s service: %s", err)
	}
	log.Printf("Deleted service")

	// And the deployment is no longer ready, so update that
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionFalse,
			Reason: "Inactive",
		})
	log.Printf("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		log.Printf("Error recording inactivation of revision: %s", err)
		return err
	}

	return nil
}

func (c *Controller) createK8SResources(rev *v1alpha1.Revision, ns string) error {
	// Fire off a Deployment..
	if err := c.reconcileDeployment(rev, ns); err != nil {
		log.Printf("Failed to create a deployment: %s", err)
		return err
	}

	// Autoscale the service
	if err := c.reconcileAutoscalerDeployment(rev); err != nil {
		log.Printf("Failed to create autoscaler Deployment: %s", err)
	}
	if err := c.reconcileAutoscalerService(rev); err != nil {
		log.Printf("Failed to create autoscaler Service: %s", err)
	}
	if c.controllerConfig.EnableVarLogCollection {
		if err := c.reconcileFluentdConfigMap(rev); err != nil {
			log.Printf("Failed to create fluent config map: %s", err)
		}
	}

	// Create k8s service
	serviceName, err := c.reconcileService(rev, ns)
	if err != nil {
		log.Printf("Failed to create k8s service: %s", err)
	} else {
		rev.Status.ServiceName = serviceName
	}

	// Check to see if the revision has already been marked as ready and
	// don't mark it if it's already ready.
	// TODO: could always fetch the endpoint again and double-check it is still
	// ready.
	if rev.Status.IsReady() {
		return nil
	}

	// Checking existing revision condition to see if it is the initial deployment or
	// during the reactivating process. If a revision is in condition "Inactive" or "Activating",
	// we need to route traffic to the activator; if a revision is in condition "Deploying",
	// we need to route traffic to the revision directly.
	reason := "Deploying"
	cond := rev.Status.GetCondition(v1alpha1.RevisionConditionReady)
	if cond != nil {
		if (cond.Reason == "Inactive" && cond.Status == corev1.ConditionFalse) ||
			(cond.Reason == "Activating" && cond.Status == corev1.ConditionUnknown) {
			reason = "Activating"
		}
	}

	// By updating our deployment status we will trigger a Reconcile()
	// that will watch for service to become ready for serving traffic.
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionUnknown,
			Reason: reason,
		})
	log.Printf("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		log.Printf("Error recording build completion: %s", err)
		return err
	}

	return nil
}

func (c *Controller) deleteDeployment(rev *v1alpha1.Revision, ns string) error {
	deploymentName := controller.GetRevisionDeploymentName(rev)
	dc := c.base.KubeClientSet.AppsV1().Deployments(ns)
	if _, err := dc.Get(deploymentName, metav1.GetOptions{}); err != nil && apierrs.IsNotFound(err) {
		return nil
	}

	log.Printf("Deleting Deployment %q", deploymentName)
	tmp := metav1.DeletePropagationForeground
	err := dc.Delete(deploymentName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		log.Printf("deployments.Delete for %q failed: %s", deploymentName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileDeployment(rev *v1alpha1.Revision, ns string) error {
	dc := c.base.KubeClientSet.AppsV1().Deployments(ns)
	// First, check if deployment exists already.
	deploymentName := controller.GetRevisionDeploymentName(rev)

	if _, err := dc.Get(deploymentName, metav1.GetOptions{}); err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("deployments.Get for %q failed: %s", deploymentName, err)
			return err
		}
		log.Printf("Deployment %q doesn't exist, creating", deploymentName)
	} else {
		// TODO(mattmoor): Compare the deployments and update if it has changed
		// out from under us.
		log.Printf("Found existing deployment %q", deploymentName)
		return nil
	}

	// Create the deployment.
	controllerRef := controller.NewRevisionControllerRef(rev)
	// Create a single pod so that it gets created before deployment->RS to try to speed
	// things up
	podSpec := MakeElaPodSpec(rev, c.controllerConfig)
	deployment := MakeElaDeployment(rev, ns)
	deployment.OwnerReferences = append(deployment.OwnerReferences, *controllerRef)

	deployment.Spec.Template.Spec = *podSpec

	// Resolve tag image references to digests.
	if err := c.resolver.Resolve(deployment); err != nil {
		glog.Errorf("Error resolving deployment: %v", err)
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionContainerHealthy,
				Status:  corev1.ConditionFalse,
				Reason:  "ContainerMissing",
				Message: err.Error(),
			})
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  "ContainerMissing",
				Message: err.Error(),
			})
		if _, err := c.updateStatus(rev); err != nil {
			log.Printf("Error recording resolution problem: %s", err)
			return err
		}
		return err
	}

	// Set the ProgressDeadlineSeconds
	deployment.Spec.ProgressDeadlineSeconds = new(int32)
	*deployment.Spec.ProgressDeadlineSeconds = progressDeadlineSeconds

	log.Printf("Creating Deployment: %q", deployment.Name)
	_, createErr := dc.Create(deployment)

	return createErr
}

func (c *Controller) deleteService(rev *v1alpha1.Revision, ns string) error {
	sc := c.base.KubeClientSet.Core().Services(ns)
	serviceName := controller.GetElaK8SServiceNameForRevision(rev)

	log.Printf("Deleting service %q", serviceName)
	tmp := metav1.DeletePropagationForeground
	err := sc.Delete(serviceName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		log.Printf("service.Delete for %q failed: %s", serviceName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileService(rev *v1alpha1.Revision, ns string) (string, error) {
	sc := c.base.KubeClientSet.Core().Services(ns)
	serviceName := controller.GetElaK8SServiceNameForRevision(rev)

	if _, err := sc.Get(serviceName, metav1.GetOptions{}); err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("services.Get for %q failed: %s", serviceName, err)
			return "", err
		}
		log.Printf("serviceName %q doesn't exist, creating", serviceName)
	} else {
		// TODO(vaikas): Check that the service is legit and matches what we expect
		// to have there.
		log.Printf("Found existing service %q", serviceName)
		return serviceName, nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	service := MakeRevisionK8sService(rev, ns)
	service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	log.Printf("Creating service: %q", service.Name)
	_, err := sc.Create(service)
	return serviceName, err
}

func (c *Controller) reconcileFluentdConfigMap(rev *v1alpha1.Revision) error {
	ns := rev.Namespace
	cmc := c.base.KubeClientSet.Core().ConfigMaps(ns)
	_, err := cmc.Get(fluentdConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("configmaps.Get for %q failed: %s", fluentdConfigMapName, err)
			return err
		}
		log.Printf("ConfigMap %q doesn't exist, creating", fluentdConfigMapName)
	} else {
		log.Printf("Found existing ConfigMap %q", fluentdConfigMapName)
		return nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	configMap := MakeFluentdConfigMap(rev, ns)
	configMap.OwnerReferences = append(configMap.OwnerReferences, *controllerRef)
	log.Printf("Creating configmap: %q", configMap.Name)
	_, err = cmc.Create(configMap)
	return err
}

func (c *Controller) deleteAutoscalerService(rev *v1alpha1.Revision) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	sc := c.base.KubeClientSet.Core().Services(AutoscalerNamespace)
	if _, err := sc.Get(autoscalerName, metav1.GetOptions{}); err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	log.Printf("Deleting autoscaler Service %q", autoscalerName)
	tmp := metav1.DeletePropagationForeground
	err := sc.Delete(autoscalerName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		log.Printf("Autoscaler Service delete for %q failed: %s", autoscalerName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileAutoscalerService(rev *v1alpha1.Revision) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	sc := c.base.KubeClientSet.Core().Services(AutoscalerNamespace)
	_, err := sc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("Autoscaler Service get for %q failed: %s", autoscalerName, err)
			return err
		}
		log.Printf("Autoscaler Service %q doesn't exist, creating", autoscalerName)
	} else {
		log.Printf("Found existing autoscaler Service %q", autoscalerName)
		return nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	service := MakeElaAutoscalerService(rev)
	service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	log.Printf("Creating autoscaler Service: %q", service.Name)
	_, err = sc.Create(service)
	return err
}

func (c *Controller) deleteAutoscalerDeployment(rev *v1alpha1.Revision) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	dc := c.base.KubeClientSet.AppsV1().Deployments(AutoscalerNamespace)
	_, err := dc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	log.Printf("Deleting autoscaler Deployment %q", autoscalerName)
	tmp := metav1.DeletePropagationForeground
	err = dc.Delete(autoscalerName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		log.Printf("Autoscaler Deployment delete for %q failed: %s", autoscalerName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileAutoscalerDeployment(rev *v1alpha1.Revision) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	dc := c.base.KubeClientSet.AppsV1().Deployments(AutoscalerNamespace)
	_, err := dc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("Autoscaler Deployment get for %q failed: %s", autoscalerName, err)
			return err
		}
		log.Printf("Autoscaler Deployment %q doesn't exist, creating", autoscalerName)
	} else {
		log.Printf("Found existing autoscaler Deployment %q", autoscalerName)
		return nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	deployment := MakeElaAutoscalerDeployment(rev, c.controllerConfig.AutoscalerImage)
	deployment.OwnerReferences = append(deployment.OwnerReferences, *controllerRef)
	log.Printf("Creating autoscaler Deployment: %q", deployment.Name)
	_, err = dc.Create(deployment)
	return err
}

func (c *Controller) removeFinalizers(rev *v1alpha1.Revision, ns string) error {
	log.Printf("Removing finalizers for %q\n", rev.Name)
	accessor, err := meta.Accessor(rev)
	if err != nil {
		log.Printf("Failed to get metadata: %s", err)
		panic("Failed to get metadata")
	}
	finalizers := accessor.GetFinalizers()
	for i, v := range finalizers {
		if v == "controller" {
			finalizers = append(finalizers[:i], finalizers[i+1:]...)
		}
	}
	accessor.SetFinalizers(finalizers)
	prClient := c.base.ElaClientSet.ElafrosV1alpha1().Revisions(rev.Namespace)
	prClient.Update(rev)
	log.Printf("The finalizer 'controller' is removed.")

	return nil
}

func (c *Controller) updateStatus(rev *v1alpha1.Revision) (*v1alpha1.Revision, error) {
	prClient := c.base.ElaClientSet.ElafrosV1alpha1().Revisions(rev.Namespace)
	newRev, err := prClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newRev.Status = rev.Status

	// TODO: for CRD there's no updatestatus, so use normal update
	return prClient.Update(newRev)
	//	return prClient.UpdateStatus(newRev)
}

// Given an endpoint see if it's managed by us and return the
// revision that created it.
// TODO: Consider using OwnerReferences.
// https://github.com/kubernetes/sample-controller/blob/master/controller.go#L373-L384
func lookupServiceOwner(endpoint *corev1.Endpoints) string {
	// see if there's a label on this object marking it as ours.
	if revisionName, ok := endpoint.Labels[ela.RevisionLabelKey]; ok {
		return revisionName
	}
	return ""
}
