/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"time"

	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	elascheme "github.com/knative/serving/pkg/client/clientset/versioned/scheme"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// Interface defines the controller interface
type Interface interface {
	Run(threadiness int, stopCh <-chan struct{}) error
}

func init() {
	// Add ela types to the default Kubernetes Scheme so Events can be
	// logged for ela types.
	elascheme.AddToScheme(scheme.Scheme)
}

// PassNew makes it simple to create an UpdateFunc for use with
// cache.ResourceEventHandlerFuncs that can delegate the same methods
// as AddFunc/DeleteFunc but passing through only the second argument
// (which is the "new" object).
func PassNew(f func(interface{})) func(interface{}, interface{}) {
	return func(first, second interface{}) {
		f(second)
	}
}

// Filter makes it simple to create FilterFunc's for use with
// cache.FilteringResourceEventHandler that filter based on the
// kind of the controlling resources.
func Filter(kind string) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		if object, ok := obj.(metav1.Object); ok {
			owner := metav1.GetControllerOf(object)
			return owner != nil &&
				owner.APIVersion == v1alpha1.SchemeGroupVersion.String() &&
				owner.Kind == kind
		}
		return false
	}
}

// Base implements most of the boilerplate and common code
// we have in our controllers.
type Base struct {
	// KubeClientSet allows us to talk to the k8s for core APIs
	KubeClientSet kubernetes.Interface

	// ServingClientSet allows us to configure Serving objects
	ServingClientSet clientset.Interface

	// BuildClientSet allows us to configure Build objects
	BuildClientSet buildclientset.Interface

	// Recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	Recorder record.EventRecorder

	// WorkQueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	WorkQueue workqueue.RateLimitingInterface

	// Sugared logger is easier to use but is not as performant as the
	// raw logger. In performance critical paths, call logger.Desugar()
	// and use the returned raw logger instead. In addition to the
	// performance benefits, raw logger also preserves type-safety at
	// the expense of slightly greater verbosity.
	Logger *zap.SugaredLogger
}

// Options defines the common controller options passed to NewBase.
// We define this to reduce the boilerplate argument list when
// creating derivative controllers.
type Options struct {
	KubeClientSet    kubernetes.Interface
	ServingClientSet clientset.Interface
	BuildClientSet   buildclientset.Interface
	Logger           *zap.SugaredLogger
}

// NewBase instantiates a new instance of Base implementing
// the common & boilerplate code between our controllers.
func NewBase(opt Options, controllerAgentName, workQueueName string) *Base {

	// Enrich the logs with controller name
	logger := opt.Logger.Named(controllerAgentName).With(zap.String(logkey.ControllerType, controllerAgentName))

	// Create event broadcaster
	logger.Debug("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logger.Named("event-broadcaster").Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: opt.KubeClientSet.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(
		scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	base := &Base{
		KubeClientSet:    opt.KubeClientSet,
		ServingClientSet: opt.ServingClientSet,
		BuildClientSet:   opt.BuildClientSet,
		Recorder:         recorder,
		WorkQueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), workQueueName),
		Logger:           logger,
	}

	return base
}

// Enqueue takes a resource and converts it into a
// namespace/name string which is then put onto the work queue.
func (c *Base) Enqueue(obj interface{}) {
	var key string
	var err error
	if key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.WorkQueue.AddRateLimited(key)
}

// EnqueueControllerOf takes a resource, identifies its controller resource, and
// converts it into a namespace/name string which is then put onto the work queue.
func (c *Base) EnqueueControllerOf(obj interface{}) {
	// TODO(mattmoor): This will not properly handle Delete, which we do
	// not currently use.  Consider using "cache.DeletedFinalStateUnknown"
	// to enqueue the last known owner.
	object, err := meta.Accessor(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// If we can determine the controller ref of this object, then
	// add that object to our workqueue.
	if owner := metav1.GetControllerOf(object); owner != nil {
		c.WorkQueue.AddRateLimited(object.GetNamespace() + "/" + owner.Name)
	}
}

// RunController will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Base) RunController(
	threadiness int,
	stopCh <-chan struct{},
	syncHandler func(string) error,
	controllerName string) error {

	defer runtime.HandleCrash()
	defer c.WorkQueue.ShutDown()

	logger := c.Logger
	logger.Infof("Starting %s controller", controllerName)

	// Launch workers to process Revision resources
	logger.Info("Starting workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			for c.processNextWorkItem(syncHandler) {
			}
		}, time.Second, stopCh)
	}

	logger.Info("Started workers")
	<-stopCh
	logger.Info("Shutting down workers")

	return nil
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Base) processNextWorkItem(syncHandler func(string) error) bool {
	obj, shutdown := c.WorkQueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.base.WorkQueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.WorkQueue.Done(obj)
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
			c.WorkQueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// resource to be synced.
		if err := syncHandler(key); err != nil {
			return fmt.Errorf("error syncing %q: %v", key, err)
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.WorkQueue.Forget(obj)
		c.Logger.Infof("Successfully synced %q", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}
