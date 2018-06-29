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

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientv1alpha1 "github.com/knative/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/route/istio"
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

	// lister indexes properties about Route
	routeLister listers.RouteLister

	// Domain configuration could change over time and access to domainConfig
	// must go through domainConfigMutex
	domainConfig      *DomainConfig
	domainConfigMutex sync.Mutex

	// Autoscale enable scale to zero experiment flag.
	enableScaleToZero *k8sflag.BoolFlag
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
	enableScaleToZero *k8sflag.BoolFlag,
) *Controller {

	// No need to lock domainConfigMutex yet since the informers that can modify
	// domainConfig haven't started yet.
	c := &Controller{
		Base:              controller.NewBase(opt, controllerAgentName, "Routes"),
		routeLister:       routeInformer.Lister(),
		enableScaleToZero: enableScaleToZero,
	}

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.Enqueue,
		UpdateFunc: func(old, new interface{}) {
			c.Enqueue(new)
		},
		DeleteFunc: c.Enqueue,
	})

	configInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.SyncConfiguration(obj.(*v1alpha1.Configuration))
		},
		UpdateFunc: func(old, new interface{}) {
			c.SyncConfiguration(new.(*v1alpha1.Configuration))
		},
	})
	opt.ConfigMapWatcher.Watch(controller.GetDomainConfigMapName(), c.receiveDomainConfig)

	return c
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, c.updateRouteEvent, "Route")
}

/////////////////////////////////////////
//  Event handlers
/////////////////////////////////////////

// updateRouteEvent compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Route resource
// with the current status of the resource.
func (c *Controller) updateRouteEvent(key string) error {
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
	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if apierrs.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("route %q in work queue no longer exists", key))
			return nil
		}
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
	if _, err := c.configureTrafficAndUpdateRouteStatus(ctx, route); err != nil {
		return err
	}
	logger.Info("Route successfully synced")
	return nil
}

// configureTrafficAndUpdateRouteStatus attempts to configure traffic based on the RouteSpec.  If there are missing
// targets (e.g. Configurations without a Ready Revision, or Revision that isn't Ready or Inactive), no traffic will be
// configured.
//
// If traffic is configured we update the RouteStatus with AllTrafficAssigned = True.  Otherwise we mark
// AllTrafficAssigned = False, with a message referring to one of the missing target.
//
// In all cases we will add annotations to the referred targets.  This is so that when they become routable we can know
// (through a listener) and attempt traffic configuration again.
func (c *Controller) configureTrafficAndUpdateRouteStatus(ctx context.Context, r *v1alpha1.Route) (*v1alpha1.Route, error) {
	r.Status.Domain = c.routeDomain(r)
	logger := logging.FromContext(ctx)
	t, targetErr := traffic.BuildTrafficConfiguration(
		c.KnativeClients().Configurations(r.Namespace), c.KnativeClients().Revisions(r.Namespace), r)
	// In all cases we will add annotations to referred targets.
	if err := c.syncLabels(ctx, r, t); err != nil {
		return r, err
	}
	if len(t.Targets) > 0 {
		logger.Info("All referred targets are routable.  Creating Istio VirtualService.")
		if err := c.reconcileVirtualService(ctx, r, istio.MakeVirtualService(r, t)); err != nil {
			return c.updateStatusErr(ctx, r, err)
		}
		logger.Info("VirtualService created, marking AllTrafficAssigned with traffic information.")
		r.Status.Traffic = t.GetTrafficTargets()
		r.Status.MarkTrafficAssigned()
		return r, nil
	}
	return r, targetErr
}

func (c *Controller) SyncConfiguration(config *v1alpha1.Configuration) {
	configName := config.Name
	ns := config.Namespace

	if config.Status.LatestReadyRevisionName == "" {
		c.Logger.Infof("Configuration %s is not ready", configName)
		return
	}
	// Check whether is configuration is referred by a route.
	routeName, ok := config.Labels[serving.RouteLabelKey]
	if !ok {
		c.Logger.Infof("Configuration %s does not have label %s", configName, serving.RouteLabelKey)
		return
	}
	// Configuration is referred by a Route.  Update such Route.
	logger := loggerWithRouteInfo(c.Logger, ns, routeName)
	ctx := logging.WithLogger(context.TODO(), logger)
	route, err := c.routeLister.Routes(ns).Get(routeName)
	if err != nil {
		logger.Error("Error fetching route upon configuration becoming ready", zap.Error(err))
		return
	}
	// Don't modify the informers copy.
	route = route.DeepCopy()
	if _, err := c.configureTrafficAndUpdateRouteStatus(ctx, route); err != nil {
		logger.Error("Error updating route upon configuration becoming ready", zap.Error(err))
	}
	c.updateStatus(ctx, route)
}

/////////////////////////////////////////
// Misc helpers.
/////////////////////////////////////////
// loggerWithRouteInfo enriches the logs with route name and namespace.
func loggerWithRouteInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Route, name))
}

func (c *Controller) getDomainConfig() *DomainConfig {
	c.domainConfigMutex.Lock()
	defer c.domainConfigMutex.Unlock()
	return c.domainConfig
}

func (c *Controller) setDomainConfig(cfg *DomainConfig) {
	c.domainConfigMutex.Lock()
	defer c.domainConfigMutex.Unlock()
	c.domainConfig = cfg
}

func (c *Controller) routeDomain(route *v1alpha1.Route) string {
	domain := c.getDomainConfig().LookupDomainForLabels(route.ObjectMeta.Labels)
	return fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, domain)
}

func (c *Controller) receiveDomainConfig(configMap *corev1.ConfigMap) {
	newDomainConfig, err := NewDomainConfigFromConfigMap(configMap)
	if err != nil {
		c.Logger.Error("Failed to parse the new config map. Previous config map will be used.",
			zap.Error(err))
		return
	}
	c.domainConfigMutex.Lock()
	defer c.domainConfigMutex.Unlock()
	c.domainConfig = newDomainConfig
}

func (c *Controller) KnativeClients() clientv1alpha1.ServingV1alpha1Interface {
	return c.ServingClientSet.ServingV1alpha1()
}
