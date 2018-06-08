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

package configuration

import (
	"fmt"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "configuration-controller"

// Receiver defines the interface that a receiver must implement to receive events
// from this Controller.
type Receiver interface {
	SyncConfiguration(*v1alpha1.Configuration) error
}

// Controller implements the controller for Configuration resources
type Controller struct {
	*controller.Base

	// lister indexes properties about Configuration
	lister listers.ConfigurationLister
	synced cache.InformerSynced

	receivers []Receiver
}

// NewController creates a new Configuration controller
func NewController(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config,
	logger *zap.SugaredLogger,
	receivers ...interface{}) (controller.Interface, error) {

	// obtain references to a shared index informer for the Configuration type.
	informer := elaInformerFactory.Serving().V1alpha1().Configurations()

	controller := &Controller{
		Base: controller.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, "Configurations", logger),
		lister: informer.Lister(),
		synced: informer.Informer().HasSynced,
	}

	for _, rcv := range receivers {
		if dr, ok := rcv.(Receiver); ok {
			controller.receivers = append(controller.receivers, dr)
		}
	}
	if len(controller.receivers) == 0 {
		return nil, fmt.Errorf("None of the provided receivers implement configuration.Receiver. " +
			"If the Configuration controller is no longer needed it should be removed.")
	}
	return controller, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh,
		[]cache.InformerSynced{c.synced}, c.syncHandler, "Configuration")
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Configuration
// resource with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the Configuration resource with this namespace/name
	original, err := c.lister.Configurations(namespace).Get(name)
	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("configuration %q in work queue no longer exists", key))
			return nil
		}

		return err
	}

	for _, dr := range c.receivers {
		// Don't modify the informer's copy, and give each receiver a fresh copy.
		cp := original.DeepCopy()
		if err := dr.SyncConfiguration(cp); err != nil {
			return err
		}
	}
	return nil
}
