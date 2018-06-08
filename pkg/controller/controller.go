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

	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	elascheme "github.com/knative/serving/pkg/client/clientset/versioned/scheme"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/receiver"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
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

// Base implements most of the boilerplate and common code
// we have in our controllers.
type Base struct {
	*receiver.Base

	// WorkQueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	WorkQueue workqueue.RateLimitingInterface
}

// NewBase instantiates a new instance of Base implementing
// the common & boilerplate code between our controllers.
func NewBase(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	informer cache.SharedIndexInformer,
	controllerAgentName string,
	workQueueName string,
	logger *zap.SugaredLogger) *Base {
	base := &Base{
		Base: receiver.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory, elaInformerFactory,
			informer, controllerAgentName, logger),
		WorkQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), workQueueName),
	}

	// Set up an event handler for when the resource types of interest change
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: base.enqueueWork,
		UpdateFunc: func(old, new interface{}) {
			base.enqueueWork(new)
		},
		DeleteFunc: base.enqueueWork,
	})

	return base
}

// enqueueWork takes a resource and converts it into a
// namespace/name string which is then put onto the work queue.
func (c *Base) enqueueWork(obj interface{}) {
	var key string
	var err error
	if key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.WorkQueue.AddRateLimited(key)
}

// RunController will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Base) RunController(
	threadiness int,
	stopCh <-chan struct{},
	informersSynced []cache.InformerSynced,
	syncHandler func(string) error,
	controllerName string) error {

	defer runtime.HandleCrash()
	defer c.WorkQueue.ShutDown()

	logger := c.Logger
	logger.Infof("Starting %s controller", controllerName)

	// Wait for the caches to be synced before starting workers
	logger.Info("Waiting for informer caches to sync")
	for i, synced := range informersSynced {
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			return fmt.Errorf("failed to wait for cache at index %v to sync", i)
		}
	}

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
