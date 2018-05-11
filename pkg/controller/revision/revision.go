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
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"

	appsv1 "k8s.io/api/apps/v1"
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

	buildv1alpha1 "github.com/elafros/build/pkg/apis/build/v1alpha1"
	buildinformers "github.com/elafros/build/pkg/client/informers/externalversions"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	listers "github.com/elafros/elafros/pkg/client/listers/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
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
	processItemCount     = stats.Int64(
		"controller_revision_queue_process_count",
		"Counter to keep track of items in the revision work queue.",
		stats.UnitNone)
	statusTagKey tag.Key
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
//TODO(vaikas): somewhat generic (generic behavior)
func NewController(
	kubeclientset kubernetes.Interface,
	elaclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	buildInformerFactory buildinformers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig *ControllerConfig) controller.Interface {

	// obtain references to a shared index informer for the Revision and
	// Endpoint type.
	informer := elaInformerFactory.Elafros().V1alpha1().Revisions()
	endpointsInformer := kubeInformerFactory.Core().V1().Endpoints()
	deploymentInformer := kubeInformerFactory.Apps().V1().Deployments()

	// Create event broadcaster
	log.Print("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:    kubeclientset,
		elaclientset:     elaclientset,
		lister:           informer.Lister(),
		synced:           informer.Informer().HasSynced,
		endpointsSynced:  endpointsInformer.Informer().HasSynced,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Revisions"),
		recorder:         recorder,
		buildtracker:     &buildTracker{builds: map[key]set{}},
		resolver:         &digestResolver{client: kubeclientset, transport: http.DefaultTransport},
		controllerConfig: controllerConfig,
	}

	log.Print("Setting up event handlers")
	// Set up an event handler for when Revision resources change
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRevision,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueRevision(new)
		},
	})

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
//TODO(grantr): generic
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	log.Print("Starting Revision controller")

	// Metrics setup: begin
	// Create the tag keys that will be used to add tags to our measurements.
	var err error
	if statusTagKey, err = tag.NewKey("status"); err != nil {
		return fmt.Errorf("failed to create tag key in OpenCensus: %v", err)
	}
	// Create view to see our measurements cumulatively.
	countView := &view.View{
		Description: "Counter to keep track of items in the revision work queue.",
		Measure:     processItemCount,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{statusTagKey},
	}
	if err = view.Register(countView); err != nil {
		return fmt.Errorf("failed to register the views in OpenCensus: %v", err)
	}
	defer view.Unregister(countView)
	// Metrics setup: end

	// Wait for the caches to be synced before starting workers
	log.Print("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	// Wait for the caches to be synced before starting workers
	log.Print("Waiting for endpoints informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.endpointsSynced); !ok {
		return fmt.Errorf("failed to wait for endpoints caches to sync")
	}

	log.Print("Starting workers")
	// Launch workers to process Revision resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	log.Print("Started workers")
	<-stopCh
	log.Print("Shutting down workers")

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
	err, processStatus := func(obj interface{}) (error, string) {
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
		// Revision resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing %q: %v", key, err), controller.PromLabelValueFailure
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		log.Printf("Successfully synced %q", key)
		return nil, controller.PromLabelValueSuccess
	}(obj)

	if ctx, tagError := tag.New(context.Background(), tag.Insert(statusTagKey, processStatus)); tagError == nil {
		// Increment the request count by one.
		stats.Record(ctx, processItemCount.M(1))
	}

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
// converge the two. It then updates the Status block of the Revision resource
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

	if err := c.updateRevisionLoggingURL(rev); err != nil {
		log.Printf("Error updating the revisions logging url: %s", err)
		return err
	}

	if rev.Spec.BuildName != "" {
		if done, failed := isBuildDone(rev); !done {
			if alreadyTracked := c.buildtracker.Track(rev); !alreadyTracked {
				if err := c.markRevisionBuilding(rev); err != nil {
					log.Printf("Error recording the BuildSucceeded=Unknown condition: %s", err)
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
	log.Printf("Marking Revision %q ready", rev.Name)
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
	log.Printf("Marking Revision %q failed", rev.Name)
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
	log.Printf("Marking Revision %q %s", rev.Name, reason)
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
		c.recorder.Event(rev, corev1.EventTypeNormal, "BuildComplete", bc.Message)
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
			log.Printf("Error fetching revision %q/%q upon build completion: %v", namespace, name, err)
		}
		if err := c.markBuildComplete(rev, cond); err != nil {
			log.Printf("Error marking build completion for %q/%q: %v", namespace, name, err)
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
		log.Printf("Error fetching revision '%s/%s': %v", namespace, revName, err)
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

	log.Printf("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		log.Printf("Error recording revision completion: %s", err)
		return
	}
	c.recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded", "Revision %s not ready due to Deployment timeout", revName)
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
		log.Printf("Error fetching revision '%s/%s': %v", namespace, revName, err)
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
		log.Printf("Endpoint %q is ready", eName)
		if err := c.markRevisionReady(rev); err != nil {
			log.Printf("Error marking revision ready for '%s/%s': %v", namespace, revName, err)
			return
		}
		c.recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon endpoint %q becoming ready", endpoint.Name)
		return
	}

	revisionAge := time.Now().Sub(getRevisionLastTransitionTime(rev))
	if revisionAge < serviceTimeoutDuration {
		return
	}

	if err := c.markRevisionFailed(rev); err != nil {
		log.Printf("Error marking revision failed for '%s/%s': %v", namespace, revName, err)
		return
	}
	c.recorder.Eventf(rev, corev1.EventTypeWarning, "RevisionFailed", "Revision did not become ready due to endpoint %q", endpoint.Name)
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
	dc := c.kubeclientset.AppsV1().Deployments(ns)
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
	dc := c.kubeclientset.AppsV1().Deployments(ns)
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
		log.Printf("Error resolving deployment: %v", err)
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
	cmc := c.kubeclientset.Core().ConfigMaps(ns)
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
	sc := c.kubeclientset.Core().Services(AutoscalerNamespace)
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
	sc := c.kubeclientset.Core().Services(AutoscalerNamespace)
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
	dc := c.kubeclientset.AppsV1().Deployments(AutoscalerNamespace)
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
	dc := c.kubeclientset.AppsV1().Deployments(AutoscalerNamespace)
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
	// see if there's a label on this object marking it as ours.
	if revisionName, ok := endpoint.Labels[ela.RevisionLabelKey]; ok {
		return revisionName
	}
	return ""
}
