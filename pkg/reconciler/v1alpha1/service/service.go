/*
Copyright 2018 The Knative Authors

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
	"context"
	"reflect"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources"
)

const controllerAgentName = "service-controller"

// Reconciler implements controller.Reconciler for Service resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	serviceLister       listers.ServiceLister
	configurationLister listers.ConfigurationLister
	routeLister         listers.RouteLister
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	opt reconciler.Options,
	serviceInformer servinginformers.ServiceInformer,
	configurationInformer servinginformers.ConfigurationInformer,
	routeInformer servinginformers.RouteInformer,
) *controller.Impl {

	c := &Reconciler{
		Base:                reconciler.NewBase(opt, controllerAgentName),
		serviceLister:       serviceInformer.Lister(),
		configurationLister: configurationInformer.Lister(),
		routeLister:         routeInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "Services", reconciler.MustNewStatsReporter("Services", c.Logger))

	c.Logger.Info("Setting up event handlers")
	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	configurationInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Service")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		},
	})

	routeInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Service")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		},
	})

	return impl
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Service resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := logging.FromContext(ctx)

	// Get the Service resource with this namespace/name
	original, err := c.serviceLister.Services(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("service %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informers copy
	service := original.DeepCopy()

	if service.Spec.Manual != nil {
		// We do not know the status when in manual mode. The Route can be
		// updated with Configurations not known to the Service which would
		// make attempts to display status potentially incorrect
		service.Status.SetManualStatus()
	} else {
		// Reconcile this copy of the service and then write back any status
		// updates regardless of whether the reconciliation errored out.
		err = c.reconcile(ctx, service)
	}
	if equality.Semantic.DeepEqual(original.Status, service.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(service); err != nil {
		logger.Warn("Failed to update service status", zap.Error(err))
		return err
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, service *v1alpha1.Service) error {
	logger := logging.FromContext(ctx)
	service.Status.InitializeConditions()

	configName := resourcenames.Configuration(service)
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
	} else if config, err = c.reconcileConfiguration(ctx, service, config); err != nil {
		logger.Errorf("Failed to reconcile Service: %q failed to reconcile Configuration: %q; %v", service.Name, configName, zap.Error(err))
		return err
	}

	// Update our Status based on the state of our underlying Configuration.
	service.Status.PropagateConfigurationStatus(config.Status)

	routeName := resourcenames.Route(service)
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
	} else if route, err = c.reconcileRoute(ctx, service, route); err != nil {
		logger.Errorf("Failed to reconcile Service: %q failed to reconcile Route: %q", service.Name, routeName)
		return err
	}

	// Update our Status based on the state of our underlying Route.
	service.Status.PropagateRouteStatus(route.Status)

	// Update the Status of the Service with the latest generation that
	// we just reconciled against so we don't keep generating Revisions.
	// TODO(#642): Remove this.
	service.Status.ObservedGeneration = service.Spec.Generation

	return nil
}

func (c *Reconciler) updateStatus(desired *v1alpha1.Service) (*v1alpha1.Service, error) {
	service, err := c.serviceLister.Services(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(service.Status, desired.Status) {
		// Don't modify the informers copy
		existing := service.DeepCopy()
		existing.Status = desired.Status
		serviceClient := c.ServingClientSet.ServingV1alpha1().Services(desired.Namespace)
		// TODO: for CRD there's no updatestatus, so use normal update.
		return serviceClient.Update(existing)
	}
	return service, nil
}

func (c *Reconciler) createConfiguration(service *v1alpha1.Service) (*v1alpha1.Configuration, error) {
	cfg, err := resources.MakeConfiguration(service)
	if err != nil {
		return nil, err
	}
	return c.ServingClientSet.ServingV1alpha1().Configurations(service.Namespace).Create(cfg)
}

func (c *Reconciler) reconcileConfiguration(ctx context.Context, service *v1alpha1.Service, config *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {

	logger := logging.FromContext(ctx)
	desiredConfig, err := resources.MakeConfiguration(service)
	if err != nil {
		return nil, err
	}

	// TODO(#642): Remove this (needed to avoid continuous updates)
	desiredConfig.Spec.Generation = config.Spec.Generation

	if equality.Semantic.DeepEqual(desiredConfig.Spec, config.Spec) {
		// No differences to reconcile.
		return config, nil
	}
	logger.Infof("Reconciling configuration diff (-desired, +observed): %v", cmp.Diff(desiredConfig.Spec, config.Spec))

	// Don't modify the informers copy
	existing := config.DeepCopy()
	// Preserve the rest of the object (e.g. ObjectMeta)
	existing.Spec = desiredConfig.Spec
	return c.ServingClientSet.ServingV1alpha1().Configurations(service.Namespace).Update(existing)
}

func (c *Reconciler) createRoute(service *v1alpha1.Service) (*v1alpha1.Route, error) {
	route, err := resources.MakeRoute(service)
	if err != nil {
		// This should be unreachable as configuration creation
		// happens first in reconcile()
		return nil, err
	}
	return c.ServingClientSet.ServingV1alpha1().Routes(service.Namespace).Create(route)
}

func (c *Reconciler) reconcileRoute(ctx context.Context, service *v1alpha1.Service, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	logger := logging.FromContext(ctx)
	desiredRoute, err := resources.MakeRoute(service)
	if err != nil {
		// This should be unreachable as configuration creation
		// happens first in reconcile()
		return nil, err
	}

	// TODO(#642): Remove this (needed to avoid continuous updates)
	desiredRoute.Spec.Generation = route.Spec.Generation

	if equality.Semantic.DeepEqual(desiredRoute.Spec, route.Spec) {
		// No differences to reconcile.
		return route, nil
	}
	logger.Infof("Reconciling route diff (-desired, +observed): %v", cmp.Diff(desiredRoute.Spec, route.Spec))

	// Don't modify the informers copy
	existing := route.DeepCopy()
	// Preserve the rest of the object (e.g. ObjectMeta)
	existing.Spec = desiredRoute.Spec
	return c.ServingClientSet.ServingV1alpha1().Routes(service.Namespace).Update(existing)
}
