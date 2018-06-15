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
	"reflect"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging/logkey"
)

const controllerAgentName = "service-controller"

// Controller implements the controller for Service resources.
type Controller struct {
	*controller.Base

	// listers index properties about resources
	serviceLister       listers.ServiceLister
	configurationLister listers.ConfigurationLister
	routeLister         listers.RouteLister
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	opt controller.Options,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config) controller.Interface {

	// obtain references to a shared index informer for the Services.
	serviceInformer := elaInformerFactory.Serving().V1alpha1().Services()
	configurationInformer := elaInformerFactory.Serving().V1alpha1().Configurations()
	routeInformer := elaInformerFactory.Serving().V1alpha1().Routes()

	informers := []cache.SharedIndexInformer{
		serviceInformer.Informer(),
		configurationInformer.Informer(),
		routeInformer.Informer(),
	}

	c := &Controller{
		Base:                controller.NewBase(opt, controllerAgentName, "Services", informers),
		serviceLister:       serviceInformer.Lister(),
		configurationLister: configurationInformer.Lister(),
		routeLister:         routeInformer.Lister(),
	}

	c.Logger.Info("Setting up event handlers")
	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.Enqueue,
		UpdateFunc: controller.PassNew(c.Enqueue),
		DeleteFunc: c.Enqueue,
	})

	configurationInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter("Service"),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(c.Enqueue),
		},
	})

	routeInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter("Service"),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(c.Enqueue),
		},
	})

	return c
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, c.Reconcile, "Service")
}

// loggerWithServiceInfo enriches the logs with service name and namespace.
func loggerWithServiceInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Service, name))
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Service resource
// with the current status of the resource.
func (c *Controller) Reconcile(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Wrap our logger with the additional context of the configuration that we are reconciling.
	logger := loggerWithServiceInfo(c.Logger, namespace, name)

	// Get the Service resource with this namespace/name
	service, err := c.serviceLister.Services(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		runtime.HandleError(fmt.Errorf("service %q in work queue no longer exists", key))
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informers copy
	service = service.DeepCopy()
	service.Status.InitializeConditions()

	configName := controller.GetServiceConfigurationName(service)
	config, err := c.configurationLister.Configurations(service.Namespace).Get(configName)
	if errors.IsNotFound(err) {
		config, err = c.createConfiguration(service)
		if err != nil {
			logger.Errorf("Failed to create Configuration %q: %v", configName, err)
			c.Recorder.Eventf(service, corev1.EventTypeWarning, "CreationFailed", "Failed to create Configuration %q: %v", configName, err)
			return err
		}
	} else if err != nil {
		logger.Errorf("Failed to reconcile Service: %q failed to Get Configuration: %q; %v", service.Name, configName, zap.Error(err))
		return err
	} else if config, err = c.reconcileConfiguration(service, config); err != nil {
		logger.Errorf("Failed to reconcile Service: %q failed to reconcile Configuration: %q; %v", service.Name, configName, zap.Error(err))
		return err
	}

	// Update our Status based on the state of our underlying Configuration.
	service.Status.PropagateConfigurationStatus(config.Status)

	routeName := controller.GetServiceRouteName(service)
	route, err := c.routeLister.Routes(service.Namespace).Get(routeName)
	if errors.IsNotFound(err) {
		route, err = c.createRoute(service)
		if err != nil {
			logger.Errorf("Failed to create Route %q: %v", routeName, err)
			c.Recorder.Eventf(service, corev1.EventTypeWarning, "CreationFailed", "Failed to create Route %q: %v", routeName, err)
			return err
		}
	} else if err != nil {
		logger.Errorf("Failed to reconcile Service: %q failed to Get Route: %q", service.Name, routeName)
		return err
	} else if route, err = c.reconcileRoute(service, route); err != nil {
		logger.Errorf("Failed to reconcile Service: %q failed to reconcile Route: %q", service.Name, routeName)
		return err
	}

	// Update our Status based on the state of our underlying Route.
	service.Status.PropagateRouteStatus(route.Status)

	// Update the Status of the Service with the latest generation that
	// we just reconciled against so we don't keep generating Revisions.
	// TODO(#642): Remove this.
	service.Status.ObservedGeneration = service.Spec.Generation

	logger.Infof("Updating the Service status:\n%+v", service)

	if _, err := c.updateStatus(service); err != nil {
		logger.Errorf("Failed to update Service %q: %v", service.Name, err)
		return err
	}

	return nil
}

func (c *Controller) updateStatus(service *v1alpha1.Service) (*v1alpha1.Service, error) {
	existing, err := c.serviceLister.Services(service.Namespace).Get(service.Name)
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(existing.Status, service.Status) {
		existing.Status = service.Status
		serviceClient := c.ElaClientSet.ServingV1alpha1().Services(service.Namespace)
		// TODO: for CRD there's no updatestatus, so use normal update.
		return serviceClient.Update(existing)
	}
	return existing, nil
}

func (c *Controller) createConfiguration(service *v1alpha1.Service) (*v1alpha1.Configuration, error) {
	cfg, err := MakeServiceConfiguration(service)
	if err != nil {
		return nil, err
	}
	return c.ElaClientSet.ServingV1alpha1().Configurations(service.Namespace).Create(cfg)
}

func (c *Controller) reconcileConfiguration(service *v1alpha1.Service, config *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {
	logger := loggerWithServiceInfo(c.Logger, service.Namespace, service.Name)
	desiredConfig, err := MakeServiceConfiguration(service)
	if err != nil {
		return nil, err
	}

	// TODO(#642): Remove this (needed to avoid continuous updates)
	desiredConfig.Spec.Generation = config.Spec.Generation

	if diff := cmp.Diff(desiredConfig.Spec, config.Spec); diff == "" {
		// No differences to reconcile.
		return config, nil
	} else {
		logger.Infof("Reconciling configuration diff (-desired,+observed): %v", diff)
	}
	// Preserve the rest of the object (e.g. ObjectMeta)
	config.Spec = desiredConfig.Spec
	return c.ElaClientSet.ServingV1alpha1().Configurations(service.Namespace).Update(config)
}

func (c *Controller) createRoute(service *v1alpha1.Service) (*v1alpha1.Route, error) {
	return c.ElaClientSet.ServingV1alpha1().Routes(service.Namespace).Create(MakeServiceRoute(service))
}

func (c *Controller) reconcileRoute(service *v1alpha1.Service, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	logger := loggerWithServiceInfo(c.Logger, service.Namespace, service.Name)
	desiredRoute := MakeServiceRoute(service)

	// TODO(#642): Remove this (needed to avoid continuous updates)
	desiredRoute.Spec.Generation = route.Spec.Generation

	if diff := cmp.Diff(desiredRoute.Spec, route.Spec); diff == "" {
		// No differences to reconcile.
		return route, nil
	} else {
		logger.Infof("Reconciling route diff (-desired,+observed): %v", diff)
	}
	// Preserve the rest of the object (e.g. ObjectMeta)
	route.Spec = desiredRoute.Spec
	return c.ElaClientSet.ServingV1alpha1().Routes(service.Namespace).Update(route)
}
