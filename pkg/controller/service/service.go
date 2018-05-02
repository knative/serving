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

package service

import (
	"fmt"

	"go.uber.org/zap"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
)

var (
	processItemCount = stats.Int64(
		"controller_service_queue_process_count",
		"Counter to keep track of items in the service work queue",
		stats.UnitNone)
	statusTagKey tag.Key
)

const (
	controllerAgentName = "service-controller"
)

// Controller implements the controller for Service resources.
// +controller:group=ela,version=v1alpha1,kind=Service,resource=services
type Controller struct {
	*controller.Base

	// lister indexes properties about Services
	lister listers.ServiceLister
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
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config,
	logger *zap.SugaredLogger) controller.Interface {

	// obtain references to a shared index informer for the Services.
	informer := elaInformerFactory.Serving().V1alpha1().Services()

	controller := &Controller{
		Base: controller.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, "Revisions", logger),
		lister: informer.Lister(),
		synced: informer.Informer().HasSynced,
	}

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, []cache.InformerSynced{c.synced},
		c.updateServiceEvent, "Service")
}

// loggerWithServiceInfo enriches the logs with service name and namespace.
func loggerWithServiceInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Service, name))
}

// updateServiceEvent compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Service resource
// with the current status of the resource.
func (c *Controller) updateServiceEvent(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	logger := loggerWithServiceInfo(c.Logger, namespace, name)

	// Get the Service resource with this namespace/name
	service, err := c.lister.Services(namespace).Get(name)
	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if apierrs.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("service %q in work queue no longer exists", key))
			return nil
		}

		return err
	}

	// Don't modify the informers copy
	service = service.DeepCopy()

	// We added the Generation to avoid fighting the Configuration controller,
	// which adds a Generation to avoid fighting the Revision controller. We
	// shouldn't need this once k8s 1.10 lands, see:
	// https://github.com/kubernetes/kubernetes/issues/58778
	// TODO(#642): Remove this.
	if service.GetGeneration() == service.Status.ObservedGeneration {
		logger.Infof("Skipping reconcile since already reconciled %d == %d",
			service.Generation, service.Status.ObservedGeneration)
		return nil
	}

	logger.Infof("Running reconcile Service for %s\n%+v\n", service.Name, service)

	config := MakeServiceConfiguration(service)
	if err := c.reconcileConfiguration(config); err != nil {
		logger.Errorf("Failed to update Configuration for %q: %v", service.Name, err)
		return err
	}

	// TODO: If revision is specified, check that the revision is ready before
	// switching routes to it. Though route controller might just do the right thing?

	route := MakeServiceRoute(service, config.Name)
	if err := c.reconcileRoute(route); err != nil {
		logger.Errorf("Failed to update Route for %q: %v", service.Name, err)
		return err
	}

	// Update the Status of the Service with the latest generation that
	// we just reconciled against so we don't keep generating Revisions.
	// TODO(#642): Remove this.
	service.Status.ObservedGeneration = service.Generation

	logger.Infof("Updating the Service status:\n%+v", service)

	if _, err := c.updateStatus(service); err != nil {
		logger.Errorf("Failed to update Service %q: %v", service.Name, err)
		return err
	}

	return nil
}

func (c *Controller) updateStatus(service *v1alpha1.Service) (*v1alpha1.Service, error) {
	serviceClient := c.ElaClientSet.ServingV1alpha1().Services(service.Namespace)
	return serviceClient.UpdateStatus(service)
}

func (c *Controller) reconcileConfiguration(config *v1alpha1.Configuration) error {
	configClient := c.ElaClientSet.ServingV1alpha1().Configurations(config.Namespace)

	existing, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			_, err := configClient.Create(config)
			return err
		}
		return err
	}

	if apiequality.Semantic.DeepEqual(existing.Spec, config.Spec) {
		return nil
	}

	copy := existing.DeepCopy()
	copy.Spec = config.Spec
	_, err = configClient.Update(copy)
	return err
}

func (c *Controller) reconcileRoute(route *v1alpha1.Route) error {
	routeClient := c.ElaClientSet.ServingV1alpha1().Routes(route.Namespace)

	existing, err := routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			_, err := routeClient.Create(route)
			return err
		}
		return err
	}

	if apiequality.Semantic.DeepEqual(existing.Spec, route.Spec) {
		return nil
	}

	copy := existing.DeepCopy()
	copy.Spec = route.Spec
	_, err = routeClient.Update(copy)
	return err
}
