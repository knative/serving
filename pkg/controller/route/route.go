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

package route

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	istioinformers "github.com/knative/serving/pkg/client/informers/externalversions/istio/v1alpha3"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	istiolisters "github.com/knative/serving/pkg/client/listers/istio/v1alpha3"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/route/config"
	"github.com/knative/serving/pkg/controller/route/resources"
	"github.com/knative/serving/pkg/controller/route/traffic"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
)

const (
	controllerAgentName = "route-controller"
)

// Controller implements the controller for Route resources.
type Controller struct {
	*controller.Base

	// Listers index properties about resources
	routeLister          listers.RouteLister
	configurationLister  listers.ConfigurationLister
	revisionLister       listers.RevisionLister
	serviceLister        corev1listers.ServiceLister
	virtualServiceLister istiolisters.VirtualServiceLister

	// Domain configuration could change over time and access to domainConfig
	// must go through domainConfigMutex
	domainConfig      *config.Domain
	domainConfigMutex sync.Mutex
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// reconcileKey - function for mapping queue keys to resource names
func NewController(
	opt controller.Options,
	routeInformer servinginformers.RouteInformer,
	configInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
	serviceInformer corev1informers.ServiceInformer,
	virtualServiceInformer istioinformers.VirtualServiceInformer,
) *Controller {

	// No need to lock domainConfigMutex yet since the informers that can modify
	// domainConfig haven't started yet.
	c := &Controller{
		Base:                 controller.NewBase(opt, controllerAgentName, "Routes"),
		routeLister:          routeInformer.Lister(),
		configurationLister:  configInformer.Lister(),
		revisionLister:       revisionInformer.Lister(),
		serviceLister:        serviceInformer.Lister(),
		virtualServiceLister: virtualServiceInformer.Lister(),
	}

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.Enqueue,
		UpdateFunc: controller.PassNew(c.Enqueue),
		DeleteFunc: c.Enqueue,
	})

	configInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.EnqueueReferringRoute,
		UpdateFunc: controller.PassNew(c.EnqueueReferringRoute),
	})

	// TODO(mattmoor): We should Reconcile Routes when controlled Services
	// and VirtualServices change.

	c.Logger.Info("Setting up ConfigMap receivers")
	opt.ConfigMapWatcher.Watch(config.DomainConfigName, c.receiveDomainConfig)
	return c
}

// Run starts the controller's worker threads, the number of which is threadiness. It then blocks until stopCh
// is closed, at which point it shuts down its internal work queue and waits for workers to finish processing their
// current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, c.Reconcile, "Route")
}

/////////////////////////////////////////
//  Event handlers
/////////////////////////////////////////

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Route resource
// with the current status of the resource.
func (c *Controller) Reconcile(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	logger := loggerWithRouteInfo(c.Logger, namespace, name)
	ctx := logging.WithLogger(context.TODO(), logger)

	// Get the Route resource with this namespace/name
	original, err := c.routeLister.Routes(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		runtime.HandleError(fmt.Errorf("route %q in work queue no longer exists", key))
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy
	route := original.DeepCopy()

	// Reconcile this copy of the route and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, route)
	if equality.Semantic.DeepEqual(original.Status, route.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(ctx, route); err != nil {
		logger.Warn("Failed to update route status", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for route %q: %v", route.Name, err)
		return err
	}
	return err
}

func (c *Controller) reconcile(ctx context.Context, route *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)
	route.Status.InitializeConditions()

	logger.Infof("Reconciling route :%v", route)
	logger.Info("Creating/Updating placeholder k8s services")
	if err := c.reconcilePlaceholderService(ctx, route); err != nil {
		return err
	}

	// Call configureTrafficAndUpdateRouteStatus, which also updates the Route.Status
	// to contain the domain we will use for Gateway creation.
	if _, err := c.configureTraffic(ctx, route); err != nil {
		return err
	}
	logger.Info("Route successfully synced")
	return nil
}

// configureTraffic attempts to configure traffic based on the RouteSpec.  If there are missing
// targets (e.g. Configurations without a Ready Revision, or Revision that isn't Ready or Inactive),
// no traffic will be configured.
//
// If traffic is configured we update the RouteStatus with AllTrafficAssigned = True.  Otherwise we
// mark AllTrafficAssigned = False, with a message referring to one of the missing target.
//
// In all cases we will add annotations to the referred targets.  This is so that when they become
// routable we can know (through a listener) and attempt traffic configuration again.
func (c *Controller) configureTraffic(ctx context.Context, r *v1alpha1.Route) (*v1alpha1.Route, error) {
	r.Status.Domain = c.routeDomain(r)
	logger := logging.FromContext(ctx)
	t, err := traffic.BuildTrafficConfiguration(c.configurationLister, c.revisionLister, r)
	badTarget, isTargetError := err.(traffic.TargetError)
	if err != nil && !isTargetError {
		// An error that's not due to missing traffic target should
		// make us fail fast.
		r.Status.MarkUnknownTrafficError(err.Error())
		return r, err
	}
	// If the only errors are missing traffic target, we need to
	// update the labels first, so that when these targets recover we
	// receive an update.
	if err := c.syncLabels(ctx, r, t); err != nil {
		return r, err
	}
	if badTarget != nil && isTargetError {
		badTarget.MarkBadTrafficTarget(&r.Status)
		return r, badTarget
	}
	logger.Info("All referred targets are routable.  Creating Istio VirtualService.")
	if err := c.reconcileVirtualService(ctx, r, resources.MakeVirtualService(r, t)); err != nil {
		return r, err
	}
	logger.Info("VirtualService created, marking AllTrafficAssigned with traffic information.")
	r.Status.Traffic = t.GetTrafficTargets()
	r.Status.MarkTrafficAssigned()
	return r, nil
}

func (c *Controller) EnqueueReferringRoute(obj interface{}) {
	config, ok := obj.(*v1alpha1.Configuration)
	if !ok {
		c.Logger.Infof("Ignoring non-Configuration objects %v", obj)
		return
	}
	if config.Status.LatestReadyRevisionName == "" {
		fmt.Printf("Configuration %s is not ready\n", config.Name)
		c.Logger.Infof("Configuration %s is not ready", config.Name)
		return
	}
	// Check whether is configuration is referred by a route.
	routeName, ok := config.Labels[serving.RouteLabelKey]
	if !ok {
		c.Logger.Infof("Configuration %s does not have a referring route", config.Name)
		return
	}
	// Configuration is referred by a Route.  Update such Route.
	route, err := c.routeLister.Routes(config.Namespace).Get(routeName)
	if err != nil {
		loggerWithRouteInfo(c.Logger, config.Namespace, routeName).Error(
			"Error fetching route upon configuration becoming ready", zap.Error(err))
		return
	}
	c.Enqueue(route)
}

/////////////////////////////////////////
// Misc helpers.
/////////////////////////////////////////
// loggerWithRouteInfo enriches the logs with route name and namespace.
func loggerWithRouteInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Route, name))
}

func (c *Controller) getDomainConfig() *config.Domain {
	c.domainConfigMutex.Lock()
	defer c.domainConfigMutex.Unlock()
	return c.domainConfig
}

func (c *Controller) routeDomain(route *v1alpha1.Route) string {
	domain := c.getDomainConfig().LookupDomainForLabels(route.ObjectMeta.Labels)
	return fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, domain)
}

func (c *Controller) receiveDomainConfig(configMap *corev1.ConfigMap) {
	newDomainConfig, err := config.NewDomainFromConfigMap(configMap)
	if err != nil {
		c.Logger.Error("Failed to parse the new config map. Previous config map will be used.",
			zap.Error(err))
		return
	}
	c.domainConfigMutex.Lock()
	defer c.domainConfigMutex.Unlock()
	c.domainConfig = newDomainConfig
}
