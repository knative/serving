/*
Copyright 2018 Google LLC

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

package autoscaling

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	"github.com/knative/serving/pkg/apis/ela/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/ela/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	controllerAgentName = "autoscaling-controller"
)

var (
	processItemCount = stats.Int64(
		"controller_autoscaling_queue_process_count",
		"Counter to keep track of items in the autoscaling work queue.",
		stats.UnitNone)
	statusTagKey tag.Key
)

// ScalerRunner wraps an autoscaler.Autoscaler and a stop channel for
// implementing shutdown behavior.
type ScalerRunner struct {
	// Scaler is the wrapped autoscaling instance.
	Scaler *autoscaler.Autoscaler
	// StopCh is a channel used to stop the associated goroutine.
	StopCh chan struct{}
}

// Controller implements the autoscaling controller for Revision resources.
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

	// scalers contains the scaler instances for each autoscaled revision.
	scalers       map[string]*ScalerRunner
	scalersMutex  sync.RWMutex
	scalersStopCh chan struct{}
}

// NewController initializes the controller. This returns *Controller instead of
// controller.Interface because the return value must expose the StatsHandler
// function.
func NewController(
	kubeclientset kubernetes.Interface,
	elaclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config) *Controller {

	// obtain references to a shared index informer for the Revision.
	informer := elaInformerFactory.Elafros().V1alpha1().Revisions()

	// Create event broadcaster
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset: kubeclientset,
		elaclientset:  elaclientset,
		lister:        informer.Lister(),
		synced:        informer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Revisions"),
		recorder:      recorder,
		scalers:       make(map[string]*ScalerRunner),
		scalersStopCh: make(chan struct{}),
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when Revision resources change
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRevision,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueRevision(new)
		},
		DeleteFunc: controller.enqueueRevision,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting Revision controller")

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
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch threadiness workers to process resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")
	close(c.scalersStopCh)

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
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
// converge the two.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	glog.Infof("Running reconcile Revision for %q:%q", namespace, name)

	// Get the Revision resource with this namespace/name
	rev, err := c.lister.Revisions(namespace).Get(name)
	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("revision %q in work queue no longer exists", key))
			c.scalersMutex.Lock()
			if scaler, exists := c.scalers[key]; exists {
				close(scaler.StopCh)
				delete(c.scalers, key)
				glog.Infof("Deleted scaler for revision %q", key)
			}
			c.scalersMutex.Unlock()
		}
		return err
	}

	// Don't modify the informer's copy.
	rev = rev.DeepCopy()

	c.scalersMutex.Lock()
	defer c.scalersMutex.Unlock()
	if _, exists := c.scalers[key]; !exists {
		scaler, err := c.createScaler(rev)
		glog.Infof("Created scaler for revision %q", key)
		if err != nil {
			return err
		}
		c.scalers[key] = scaler
	}

	return nil
}

func (c *Controller) createScaler(rev *v1alpha1.Revision) (*ScalerRunner, error) {
	//TODO CRD for these things
	config := autoscaler.Config{
		TargetConcurrency:    1.0,
		MaxScaleUpRate:       10,
		StableWindow:         time.Second * 60,
		PanicWindow:          time.Second * 6,
		ScaleToZeroThreshold: time.Minute * 5,
	}
	controller := metav1.GetControllerOf(rev)
	reporter, err := autoscaler.NewStatsReporter(rev.Namespace, controller.Name, rev.Name)
	if err != nil {
		return nil, err
	}

	scaler := autoscaler.NewAutoscaler(config, reporter)
	stopCh := make(chan struct{})
	runner := &ScalerRunner{Scaler: scaler, StopCh: stopCh}

	//TODO configurable tick
	ticker := time.NewTicker(time.Second * 2)

	go func() {
		for {
			select {
			case <-c.scalersStopCh:
				return
			case <-stopCh:
				return
			case <-ticker.C:
				c.tickScaler(rev, scaler)
			}
		}
	}()

	return runner, nil
}

// RecordStat records a stat for the given revision. revKey should have the
// form namespace/name.
func (c *Controller) RecordStat(revKey string, stat autoscaler.Stat) {
	c.scalersMutex.RLock()
	defer c.scalersMutex.RUnlock()
	scaler, exists := c.scalers[revKey]
	if exists {
		scaler.Scaler.Record(stat)
	}
}

func (c *Controller) tickScaler(rev *v1alpha1.Revision, scaler *autoscaler.Autoscaler) {
	desiredScale, scaled := scaler.Scale(time.Now())

	if scaled {
		// Don't allow scaling to zero
		if !(desiredScale > 0) {
			glog.Infof("Cannot scale %s/%s: desiredScale > 0 is false", rev.Namespace, rev.Name)
			return
		}

		// Don't scale at all if deployment is scaled to zero
		// TODO this should use a deployment lister
		deploymentName := controller.GetRevisionDeploymentName(rev)
		deployment, err := c.kubeclientset.AppsV1().Deployments(rev.Namespace).Get(deploymentName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Error getting deployment %s: %v", deploymentName, err)
			return
		}
		//TODO use the generic scale client
		//TODO use the revision scale resource
		if !(*deployment.Spec.Replicas > 0) {
			glog.Infof("Cannot scale %s/%s: deployment.Spec.Replicas > 0 is false", rev.Namespace, rev.Name)
			return
		}

		//TODO use the generic scale client
		deployment.Spec.Replicas = &desiredScale
		_, err = c.kubeclientset.AppsV1().Deployments(rev.Namespace).Update(deployment)
		if err != nil {
			glog.Errorf("Error scaling deployment %s: %v", deploymentName, err)
		}
	}
}

// StatsHandler exposes a websocket handler for receiving stats from queue
// sidecar containers.
func (c *Controller) StatsHandler(w http.ResponseWriter, r *http.Request) {
	var upgrader websocket.Upgrader
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		glog.Errorf("Error upgrading websocket: %v", err)
		return
	}

	go func() {
		<-c.scalersStopCh
		// Send a close message to tell the client to immediately reconnect
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1012, "Restarting"))
		conn.Close()
	}()

	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			glog.Error("Dropping non-binary message.")
			continue
		}
		dec := gob.NewDecoder(bytes.NewBuffer(msg))
		var sm autoscaler.StatMessage
		err = dec.Decode(&sm)
		if err != nil {
			glog.Error(err)
			continue
		}

		c.RecordStat(sm.RevisionKey, sm.Stat)
	}
}
