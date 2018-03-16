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
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	buildv1alpha1 "github.com/elafros/elafros/pkg/apis/build/v1alpha1"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	listers "github.com/elafros/elafros/pkg/client/listers/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"
)

var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("Revision")

const (
	routeLabel      string = "route"
	elaVersionLabel string = "revision"

	elaContainerName string = "ela-container"
	elaPortName      string = "ela-port"
	elaPort                 = 8080

	elaContainerLogVolumeName      string = "ela-logs"
	elaContainerLogVolumeMountPath string = "/var/log/app_engine"

	queueContainerName string = "queue-proxy"
	// queueSidecarName set by -queueSidecarName flag
	queueHttpPortName string = "queue-http-port"

	requestQueueContainerName string = "request-queue"
	requestQueuePortName      string = "queue-port"
	requestQueuePort                 = 8012

	controllerAgentName = "revision-controller"

	autoscalerPort = 8080
)

var (
	elaPodReplicaCount       = int32(1)
	elaPodMaxUnavailable     = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
	elaPodMaxSurge           = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
	queueSidecarImage        string
	revisionProcessItemCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "elafros",
		Name:      "revision_process_item_count",
		Help:      "Counter to keep track of items in the revision work queue",
	}, []string{"status"})
)

func init() {
	prometheus.MustRegister(revisionProcessItemCount)
	flag.StringVar(&queueSidecarImage, "queueSidecarImage", "", "The digest of the queue sidecar image.")
}

// Helper to make sure we log error messages returned by Reconcile().
func printErr(err error) error {
	if err != nil {
		log.Printf("Logging error: %s", err)
	}
	return err
}

// Controller implements the controller for Revision resources.
// +controller:group=ela,version=v1alpha1,kind=Revision,resource=revisions
type Controller struct {
	// kubeClient allows us to talk to the k8s for core APIs
	kubeclientset kubernetes.Interface

	// elaClient allows us to configure Ela objects
	elaclientset clientset.Interface

	// lister indexes properties about Revision
	lister listers.RevisionLister
	synced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	buildtracker *buildTracker

	// don't start the workers until endpoints cache have been synced
	endpointsSynced cache.InformerSynced
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// queue - message queue for handling new events.  unique to this controller.
//TODO(vaikas): somewhat generic (generic behavior)
func NewController(
	kubeclientset kubernetes.Interface,
	elaclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	_ controller.Config) controller.Interface {

	// obtain references to a shared index informer for the Revision and
	// Endpoint type.
	informer := elaInformerFactory.Elafros().V1alpha1().Revisions()
	endpointsInformer := kubeInformerFactory.Core().V1().Endpoints()

	// Create event broadcaster
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:   kubeclientset,
		elaclientset:    elaclientset,
		lister:          informer.Lister(),
		synced:          informer.Informer().HasSynced,
		endpointsSynced: endpointsInformer.Informer().HasSynced,
		workqueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Revisions"),
		recorder:        recorder,
		buildtracker:    &buildTracker{builds: map[key]set{}},
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when Revision resources change
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRevision,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueRevision(new)
		},
	})

	// Obtain a reference to a shared index informer for the Build type.
	buildInformer := elaInformerFactory.Build().V1alpha1().Builds()
	buildInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.addBuildEvent,
		UpdateFunc: controller.updateBuildEvent,
	})

	endpointsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.addEndpointsEvent,
		UpdateFunc: controller.updateEndpointsEvent,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
//TODO(grantr): generic
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting Revision controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for endpoints informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.endpointsSynced); !ok {
		return fmt.Errorf("failed to wait for endpoints caches to sync")
	}

	glog.Info("Starting workers")
	// Launch threadiness workers to process resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
//TODO(grantr): generic
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
//TODO(vaikas): generic
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err, promStatus := func(obj interface{}) (error, string) {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil, controller.PromLabelValueInvalid
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing %q: %v", key, err), controller.PromLabelValueFailure
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced %q", key)
		return nil, controller.PromLabelValueSuccess
	}(obj)

	revisionProcessItemCount.With(prometheus.Labels{"status": promStatus}).Inc()

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// enqueueRevision takes a Revision resource and
// converts it into a namespace/name string which is then put onto the work
// queue. This method should *not* be passed resources of any type other than
// Revision.
//TODO(grantr): generic
func (c *Controller) enqueueRevision(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Foo resource
// with the current status of the resource.
//TODO(grantr): not generic
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

	if rev.Spec.BuildName != "" {
		if done, failed := isBuildDone(rev); !done {
			if alreadyTracked := c.buildtracker.Track(rev); !alreadyTracked {
				rev.Status.SetCondition(
					&v1alpha1.RevisionCondition{
						Type:   v1alpha1.RevisionConditionBuildComplete,
						Status: corev1.ConditionFalse,
						Reason: "Building",
					})
				// Let this trigger a reconciliation loop.
				if _, err := c.updateStatus(rev); err != nil {
					glog.Errorf("Error recording the BuildComplete=False condition: %s", err)
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

	ns, err := controller.GetOrCreateRevisionNamespace(namespace, c.kubeclientset)
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

// Checks whether the Revision knows whether the build is done.
// TODO(mattmoor): Use a method on the Build type.
func isBuildDone(rev *v1alpha1.Revision) (done, failed bool) {
	for _, cond := range rev.Status.Conditions {
		if cond.Status != corev1.ConditionTrue {
			continue
		}
		switch cond.Type {
		case v1alpha1.RevisionConditionBuildComplete:
			return true, false
		case v1alpha1.RevisionConditionBuildFailed:
			return true, true
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

func (c *Controller) markBuildComplete(rev *v1alpha1.Revision, bc *buildv1alpha1.BuildCondition) error {
	switch bc.Type {
	case buildv1alpha1.BuildComplete:
		rev.Status.RemoveCondition(v1alpha1.RevisionConditionBuildFailed)
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:   v1alpha1.RevisionConditionBuildComplete,
				Status: corev1.ConditionTrue,
			})
		c.recorder.Event(rev, corev1.EventTypeNormal, "BuildComplete", bc.Message)
	case buildv1alpha1.BuildFailed, buildv1alpha1.BuildInvalid:
		rev.Status.RemoveCondition(v1alpha1.RevisionConditionBuildComplete)
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionBuildFailed,
				Status:  corev1.ConditionTrue,
				Reason:  bc.Reason,
				Message: bc.Message,
			})
		c.recorder.Event(rev, corev1.EventTypeWarning, "BuildFailed", bc.Message)
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

	if !getIsServiceReady(endpoint) {
		// The service isn't ready, so ignore this event.
		glog.Infof("Endpoint %q is not ready", eName)
		return
	}
	glog.Infof("Endpoint %q is ready", eName)

	rev, err := c.lister.Revisions(namespace).Get(revName)
	if err != nil {
		glog.Errorf("Error fetching revision '%s/%s' upon service becoming ready: %v", namespace, revName, err)
		return
	}

	// Check to see if the revision has already been marked as ready and if
	// it is, then there's no need to do anything to it.
	if rev.Status.IsReady() {
		return
	}

	// Don't modify the informer's copy.
	rev = rev.DeepCopy()

	if err := c.markRevisionReady(rev); err != nil {
		glog.Errorf("Error marking revision ready for '%s/%s': %v", namespace, revName, err)
	} else {
		c.recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon endpoint %q becoming ready", endpoint.Name)
	}
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

	if deletionTimestamp == nil {
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

	err = c.deleteAutoscalerDeployment(rev, ns)
	if err != nil {
		log.Printf("Failed to delete autoscaler Deployment: %s", err)
	}
	log.Printf("Deleted autoscaler Deployment")

	err = c.deleteAutoscalerService(rev, ns)
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
	err := c.reconcileDeployment(rev, ns)
	if err != nil {
		log.Printf("Failed to create a deployment: %s", err)
		return err
	}

	// Autoscale the service
	err = c.reconcileAutoscalerDeployment(rev, ns)
	if err != nil {
		log.Printf("Failed to create autoscaler Deployment: %s", err)
	}
	err = c.reconcileAutoscalerService(rev, ns)
	if err != nil {
		log.Printf("Failed to create autoscaler Service: %s", err)
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

	// By updating our deployment status we will trigger a Reconcile()
	// that will watch for service to become ready for serving traffic.
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionFalse,
			Reason: "Deploying",
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
	dc := c.kubeclientset.ExtensionsV1beta1().Deployments(ns)
	_, err := dc.Get(deploymentName, metav1.GetOptions{})
	if err != nil && apierrs.IsNotFound(err) {
		return nil
	}

	log.Printf("Deleting Deployment %q", deploymentName)
	tmp := metav1.DeletePropagationForeground
	err = dc.Delete(deploymentName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		log.Printf("deployments.Delete for %q failed: %s", deploymentName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileDeployment(rev *v1alpha1.Revision, ns string) error {
	//TODO(grantr): migrate this to AppsV1 when it goes GA. See
	// https://kubernetes.io/docs/reference/workloads-18-19.
	dc := c.kubeclientset.ExtensionsV1beta1().Deployments(ns)

	// First, check if deployment exists already.
	deploymentName := controller.GetRevisionDeploymentName(rev)
	_, err := dc.Get(deploymentName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("deployments.Get for %q failed: %s", deploymentName, err)
			return err
		}
		log.Printf("Deployment %q doesn't exist, creating", deploymentName)
	} else {
		log.Printf("Found existing deployment %q", deploymentName)
		return nil
	}

	// Create the deployment.
	controllerRef := metav1.NewControllerRef(rev, controllerKind)
	// Create a single pod so that it gets created before deployment->RS to try to speed
	// things up
	podSpec := MakeElaPodSpec(rev)
	deployment := MakeElaDeployment(rev, ns)
	deployment.OwnerReferences = append(deployment.OwnerReferences, *controllerRef)
	deployment.Spec.Template.Spec = *podSpec

	log.Printf("Creating Deployment: %q", deployment.Name)
	_, createErr := dc.Create(deployment)
	return createErr
}

func (c *Controller) deleteService(rev *v1alpha1.Revision, ns string) error {
	sc := c.kubeclientset.Core().Services(ns)
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
	sc := c.kubeclientset.Core().Services(ns)
	serviceName := controller.GetElaK8SServiceNameForRevision(rev)
	_, err := sc.Get(serviceName, metav1.GetOptions{})
	if err != nil {
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

	controllerRef := metav1.NewControllerRef(rev, controllerKind)
	service := MakeRevisionK8sService(rev, ns)
	service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	log.Printf("Creating service: %q", service.Name)
	_, err = sc.Create(service)
	return serviceName, err
}

func (c *Controller) deleteAutoscalerService(rev *v1alpha1.Revision, ns string) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	sc := c.kubeclientset.Core().Services(ns)
	_, err := sc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	log.Printf("Deleting autoscaler Service %q", autoscalerName)
	tmp := metav1.DeletePropagationForeground
	err = sc.Delete(autoscalerName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		log.Printf("Autoscaler Service delete for %q failed: %s", autoscalerName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileAutoscalerService(rev *v1alpha1.Revision, ns string) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	sc := c.kubeclientset.Core().Services(ns)
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

	controllerRef := metav1.NewControllerRef(rev, controllerKind)
	service := MakeElaAutoscalerService(rev, ns)
	service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	log.Printf("Creating autoscaler Service: %q", service.Name)
	_, err = sc.Create(service)
	return err
}

func (c *Controller) deleteAutoscalerDeployment(rev *v1alpha1.Revision, ns string) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	dc := c.kubeclientset.ExtensionsV1beta1().Deployments(ns)
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

func (c *Controller) reconcileAutoscalerDeployment(rev *v1alpha1.Revision, ns string) error {
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	dc := c.kubeclientset.ExtensionsV1beta1().Deployments(ns)
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

	controllerRef := metav1.NewControllerRef(rev, controllerKind)
	deployment := MakeElaAutoscalerDeployment(rev, ns)
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
	prClient := c.elaclientset.ElafrosV1alpha1().Revisions(rev.Namespace)
	prClient.Update(rev)
	log.Printf("The finalizer 'controller' is removed.")

	return nil
}

func (c *Controller) updateStatus(rev *v1alpha1.Revision) (*v1alpha1.Revision, error) {
	prClient := c.elaclientset.ElafrosV1alpha1().Revisions(rev.Namespace)
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
	// see if there's a 'revision' label on this object marking it as ours.
	if revisionName, ok := endpoint.Labels["revision"]; ok {
		return revisionName
	}
	return ""
}
