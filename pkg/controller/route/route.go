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

package route

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/knative/serving/pkg"

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.uber.org/zap"
)

var (
	processItemCount = stats.Int64(
		"controller_route_queue_process_count",
		"Counter to keep track of items in the route work queue.",
		stats.UnitNone)
	statusTagKey tag.Key
)

const (
	controllerAgentName = "route-controller"
)

// RevisionRoute represents a single target to route to.
// Basically represents a k8s service representing a specific Revision
// and how much of the traffic goes to it.
type RevisionRoute struct {
	// Name for external routing. Optional
	Name string
	// RevisionName is the underlying revision that we're currently
	// routing to. Could be resolved from the Configuration or
	// specified explicitly in TrafficTarget
	RevisionName string
	// Service is the name of the k8s service we route to.
	// Note we should not put service namespace as a suffix in this field.
	Service string
	// The k8s service namespace
	Namespace string
	Weight    int
}

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
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	servingSystemInformerFactory kubeinformers.SharedInformerFactory,
	config *rest.Config,
	enableScaleToZero *k8sflag.BoolFlag) controller.Interface {

	// obtain references to a shared index informer for the Routes and
	// Configurations type.
	routeInformer := elaInformerFactory.Serving().V1alpha1().Routes()
	configInformer := elaInformerFactory.Serving().V1alpha1().Configurations()
	ingressInformer := kubeInformerFactory.Extensions().V1beta1().Ingresses()
	configMapInformer := servingSystemInformerFactory.Core().V1().ConfigMaps()

	informers := []cache.SharedIndexInformer{
		routeInformer.Informer(),
		configInformer.Informer(),
		ingressInformer.Informer(),
		configMapInformer.Informer(),
	}

	domainConfig, err := NewDomainConfig(opt.KubeClientSet)
	if err != nil {
		opt.Logger.Fatalf("Error loading domain config: %v", err)
	}

	// No need to lock domainConfigMutex yet since the informers that can modify
	// domainConfig haven't started yet.
	controller := &Controller{
		Base:              controller.NewBase(opt, controllerAgentName, "Routes", informers),
		routeLister:       routeInformer.Lister(),
		domainConfig:      domainConfig,
		enableScaleToZero: enableScaleToZero,
	}

	controller.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.Enqueue,
		UpdateFunc: func(old, new interface{}) {
			controller.Enqueue(new)
		},
		DeleteFunc: controller.Enqueue,
	})

	configInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.SyncConfiguration(obj.(*v1alpha1.Configuration))
		},
		UpdateFunc: func(old, new interface{}) {
			controller.SyncConfiguration(new.(*v1alpha1.Configuration))
		},
	})
	// v1beta1.Ingress
	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.SyncIngress(obj.(*v1beta1.Ingress))
		},
		UpdateFunc: func(old, new interface{}) {
			controller.SyncIngress(new.(*v1beta1.Ingress))
		},
	})
	configMapInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.SyncConfigMap(obj.(*corev1.ConfigMap))
		},
		UpdateFunc: func(old, new interface{}) {
			controller.SyncConfigMap(new.(*corev1.ConfigMap))
		},
	})
	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, c.updateRouteEvent, "Route")
}

// loggerWithRouteInfo enriches the logs with route name and namespace.
func loggerWithRouteInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Route, name))
}

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
	route, err := c.routeLister.Routes(namespace).Get(name)
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
	route = route.DeepCopy()
	route.Status.InitializeConditions()

	logger.Infof("Reconciling route :%v", route)

	// Create a placeholder service that is simply used by istio as a placeholder.
	// This service could eventually be the 'activator' service that will get all the
	// fallthrough traffic if there are no route rules (revisions to target).
	// This is one way to implement the 0->1. For now, we'll just create a placeholder
	// that selects nothing.
	logger.Info("Creating/Updating placeholder k8s services")

	if err := c.reconcilePlaceholderService(ctx, route); err != nil {
		return err
	}

	// Call syncTrafficTargetsAndUpdateRouteStatus, which also updates the Route.Status
	// to contain the domain we will use for Ingress creation.

	if _, err := c.syncTrafficTargetsAndUpdateRouteStatus(ctx, route); err != nil {
		return err
	}

	// Then create or update the Ingress rule for this service
	logger.Info("Creating or updating ingress rule")
	if err := c.reconcileIngress(ctx, route); err != nil {
		logger.Error("Failed to create or update ingress rule", zap.Error(err))
		return err
	}

	logger.Info("Route successfully synced")
	_, err = c.updateStatus(ctx, route)
	return err
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

// syncTrafficTargetsAndUpdateRouteStatus attempts to converge the actual state and desired state
// according to the traffic targets in Spec field for Route resource. It then updates the Status
// block of the Route and returns the updated one.
func (c *Controller) syncTrafficTargetsAndUpdateRouteStatus(ctx context.Context, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	logger := logging.FromContext(ctx)
	c.consolidateTrafficTargets(ctx, route)
	configMap, revMap, err := c.getDirectTrafficTargets(ctx, route)
	if err != nil {
		return nil, err
	}
	if err := c.extendConfigurationsWithIndirectTrafficTargets(ctx, route, configMap, revMap); err != nil {
		return nil, err
	}
	if err := c.extendRevisionsWithIndirectTrafficTargets(ctx, route, configMap, revMap); err != nil {
		return nil, err
	}

	if err := c.deleteLabelForOutsideOfGivenConfigurations(ctx, route, configMap); err != nil {
		return nil, err
	}
	if err := c.setLabelForGivenConfigurations(ctx, route, configMap); err != nil {
		return nil, err
	}

	if err := c.deleteLabelForOutsideOfGivenRevisions(ctx, route, revMap); err != nil {
		return nil, err
	}
	if err := c.setLabelForGivenRevisions(ctx, route, revMap); err != nil {
		return nil, err
	}

	// Then create the actual route rules.
	logger.Info("Creating Istio route rules")
	revisionRoutes, inactiveRev, err := c.createOrUpdateRouteRules(ctx, route, configMap, revMap)
	if err != nil {
		logger.Error("Failed to create routes", zap.Error(err))
		return nil, err
	}

	// If revision routes were configured, update them
	if revisionRoutes != nil {
		traffic := []v1alpha1.TrafficTarget{}
		for _, r := range revisionRoutes {
			traffic = append(traffic, v1alpha1.TrafficTarget{
				Name:         r.Name,
				RevisionName: r.RevisionName,
				Percent:      r.Weight,
			})
		}
		route.Status.Traffic = traffic
	}
	route.Status.Domain = c.routeDomain(route)
	newRoute, err := c.updateStatus(ctx, route)
	if err != nil {
		logger.Error("Failed to update routes", zap.Error(err))
		return nil, err
	}
	// After the route rules are updated, mark the revision inactive. In case of deactivation,
	// the routerules need to point to activator-service before we deactivate the deployment.
	if inactiveRev != "" {
		revisionClient := c.ElaClientSet.ServingV1alpha1().Revisions(route.Namespace)
		rev, err := revisionClient.Get(inactiveRev, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("Failed to fetch the inactive revision %s: %s", inactiveRev, err)
			return nil, err
		}
		rev.Status.MarkInactive()
		if _, err = revisionClient.Update(rev); err != nil {
			logger.Errorf("Failed to update revision %s to be inactive: %s", inactiveRev, err)
			return nil, err
		}
	}
	return newRoute, nil
}

func (c *Controller) reconcilePlaceholderService(ctx context.Context, route *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)
	service := MakeRouteK8SService(route)
	if _, err := c.KubeClientSet.CoreV1().Services(route.Namespace).Create(service); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// Service already exist.
			return nil
		}
		logger.Error("Failed to create service", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create service %q: %v", service.Name, err)
		return err
	}
	logger.Infof("Created service %s", service.Name)
	c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created service %q", service.Name)
	return nil
}

func (c *Controller) reconcileIngress(ctx context.Context, route *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)
	ingressNamespace := route.Namespace
	ingressName := controller.GetElaK8SIngressName(route)
	ingress := MakeRouteIngress(route)
	ingressClient := c.KubeClientSet.ExtensionsV1beta1().Ingresses(ingressNamespace)
	existing, err := ingressClient.Get(ingressName, metav1.GetOptions{})

	if err != nil {
		if apierrs.IsNotFound(err) {
			if _, err = ingressClient.Create(ingress); err == nil {
				logger.Infof("Created ingress %q in namespace %q", ingressName, ingressNamespace)
				c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created Ingress %q in namespace %q", ingressName, ingressNamespace)
			}
		}
		return err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(existing.Spec, ingress.Spec) {
		existing.Spec = ingress.Spec
		if _, err = ingressClient.Update(existing); err == nil {
			logger.Infof("Updated ingress %q in namespace %q", ingressName, ingressNamespace)
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated Ingress %q in namespace %q", ingressName, ingressNamespace)
		}
		return err
	}
	return nil
}

func (c *Controller) getDirectTrafficTargets(ctx context.Context, route *v1alpha1.Route) (
	map[string]*v1alpha1.Configuration, map[string]*v1alpha1.Revision, error) {
	logger := logging.FromContext(ctx)
	ns := route.Namespace
	configClient := c.ElaClientSet.ServingV1alpha1().Configurations(ns)
	revClient := c.ElaClientSet.ServingV1alpha1().Revisions(ns)
	configMap := map[string]*v1alpha1.Configuration{}
	revMap := map[string]*v1alpha1.Revision{}

	for _, tt := range route.Spec.Traffic {
		if tt.ConfigurationName != "" {
			configName := tt.ConfigurationName
			config, err := configClient.Get(configName, metav1.GetOptions{})
			if err != nil {
				logger.Infof("Failed to fetch Configuration %q: %v", configName, err)
				if apierrs.IsNotFound(err) {
					route.Status.MarkTrafficNotAssigned("Configuration", configName)
					if _, err := c.updateStatus(ctx, route); err != nil {
						// If we failed to update the status, return that error instead of the "config not found" error.
						return nil, nil, err
					}
				}
				return nil, nil, err
			}
			configMap[configName] = config
		} else {
			revName := tt.RevisionName
			rev, err := revClient.Get(revName, metav1.GetOptions{})
			if err != nil {
				logger.Infof("Failed to fetch Revision %q: %v", revName, err)
				if apierrs.IsNotFound(err) {
					route.Status.MarkTrafficNotAssigned("Revision", revName)
					if _, err := c.updateStatus(ctx, route); err != nil {
						// If we failed to update the status, return that error instead of the "revision not found" error.
						return nil, nil, err
					}
				}
				return nil, nil, err
			}
			revMap[revName] = rev
		}
	}

	return configMap, revMap, nil
}

func (c *Controller) extendConfigurationsWithIndirectTrafficTargets(
	ctx context.Context, route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration,
	revMap map[string]*v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	ns := route.Namespace
	configClient := c.ElaClientSet.ServingV1alpha1().Configurations(ns)

	// Get indirect configurations.
	for _, rev := range revMap {
		if configName, ok := rev.Labels[serving.ConfigurationLabelKey]; ok {
			if _, ok := configMap[configName]; !ok {
				// This is not a duplicated configuration
				config, err := configClient.Get(configName, metav1.GetOptions{})
				if err != nil {
					logger.Errorf("Failed to fetch Configuration %s: %s", configName, err)
					return err
				}
				configMap[configName] = config
			}
		} else {
			logger.Infof("Revision %s does not have label %s", rev.Name, serving.ConfigurationLabelKey)
		}
	}

	return nil
}

func (c *Controller) extendRevisionsWithIndirectTrafficTargets(
	ctx context.Context, route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration,
	revMap map[string]*v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	ns := route.Namespace
	revisionClient := c.ElaClientSet.ServingV1alpha1().Revisions(ns)

	allTrafficAssigned := true
	for _, tt := range route.Spec.Traffic {
		configName := tt.ConfigurationName
		if tt.ConfigurationName == "" {
			continue
		}
		config, ok := configMap[configName]
		if !ok {
			continue
		}
		revName := config.Status.LatestReadyRevisionName
		if revName == "" {
			logger.Infof("Configuration %s is not ready, skipping.", tt.ConfigurationName)
			allTrafficAssigned = false
			continue
		}
		if _, ok := revMap[revName]; ok {
			// Skip duplicated revisions
			continue
		}
		rev, err := revisionClient.Get(revName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("Failed to fetch Revision %s: %s", revName, err)
			if apierrs.IsNotFound(err) {
				route.Status.MarkTrafficNotAssigned("Revision", revName)
				if _, err := c.updateStatus(ctx, route); err != nil {
					// If we failed to update the status, return that error instead of the "revision not found" error.
					return err
				}
			}
			return err
		}
		revMap[revName] = rev
	}

	if allTrafficAssigned {
		route.Status.MarkTrafficAssigned()
	}

	return nil
}

func (c *Controller) setLabelForGivenConfigurations(
	ctx context.Context, route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration) error {
	logger := logging.FromContext(ctx)
	configClient := c.ElaClientSet.ServingV1alpha1().Configurations(route.Namespace)

	// Validate
	for _, config := range configMap {
		if routeName, ok := config.Labels[serving.RouteLabelKey]; ok {
			// TODO(yanweiguo): add a condition in status for this error
			if routeName != route.Name {
				errMsg := fmt.Sprintf("Configuration %q is already in use by %q, and cannot be used by %q",
					config.Name, routeName, route.Name)
				c.Recorder.Event(route, corev1.EventTypeWarning, "ConfigurationInUse", errMsg)
				logger.Error(errMsg)
				return errors.New(errMsg)
			}
		}
	}

	// Set label for newly added configurations as traffic target.
	for _, config := range configMap {
		if config.Labels == nil {
			config.Labels = make(map[string]string)
		} else if _, ok := config.Labels[serving.RouteLabelKey]; ok {
			continue
		}
		config.Labels[serving.RouteLabelKey] = route.Name
		if _, err := configClient.Update(config); err != nil {
			logger.Errorf("Failed to update Configuration %s: %s", config.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) setLabelForGivenRevisions(
	ctx context.Context, route *v1alpha1.Route, revMap map[string]*v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	revisionClient := c.ElaClientSet.ServingV1alpha1().Revisions(route.Namespace)

	// Validate revision if it already has a route label
	for _, rev := range revMap {
		if routeName, ok := rev.Labels[serving.RouteLabelKey]; ok {
			if routeName != route.Name {
				errMsg := fmt.Sprintf("Revision %q is already in use by %q, and cannot be used by %q",
					rev.Name, routeName, route.Name)
				c.Recorder.Event(route, corev1.EventTypeWarning, "RevisionInUse", errMsg)
				logger.Error(errMsg)
				return errors.New(errMsg)
			}
		}
	}

	for _, rev := range revMap {
		if rev.Labels != nil {
			if _, ok := rev.Labels[serving.RouteLabelKey]; ok {
				continue
			}
		}
		// Fetch the latest version of the revision to label, to narrow the window for
		// optimistic concurrency failures.
		latestRev, err := revisionClient.Get(rev.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if latestRev.Labels == nil {
			latestRev.Labels = make(map[string]string)
		} else if _, ok := latestRev.Labels[serving.RouteLabelKey]; ok {
			continue
		}
		latestRev.Labels[serving.RouteLabelKey] = route.Name
		if _, err := revisionClient.Update(latestRev); err != nil {
			logger.Errorf("Failed to add route label to Revision %s: %s", rev.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) deleteLabelForOutsideOfGivenConfigurations(
	ctx context.Context, route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration) error {
	logger := logging.FromContext(ctx)
	configClient := c.ElaClientSet.ServingV1alpha1().Configurations(route.Namespace)
	// Get Configurations set as traffic target before this sync.
	oldConfigsList, err := configClient.List(
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", serving.RouteLabelKey, route.Name),
		},
	)
	if err != nil {
		logger.Errorf("Failed to fetch configurations with label '%s=%s': %s",
			serving.RouteLabelKey, route.Name, err)
		return err
	}

	// Delete label for newly removed configurations as traffic target.
	for _, config := range oldConfigsList.Items {
		if _, ok := configMap[config.Name]; !ok {
			delete(config.Labels, serving.RouteLabelKey)
			if _, err := configClient.Update(&config); err != nil {
				logger.Errorf("Failed to update Configuration %s: %s", config.Name, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) deleteLabelForOutsideOfGivenRevisions(
	ctx context.Context, route *v1alpha1.Route, revMap map[string]*v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	revClient := c.ElaClientSet.ServingV1alpha1().Revisions(route.Namespace)

	oldRevList, err := revClient.List(
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", serving.RouteLabelKey, route.Name),
		},
	)
	if err != nil {
		logger.Errorf("Failed to fetch revisions with label '%s=%s': %s",
			serving.RouteLabelKey, route.Name, err)
		return err
	}

	// Delete label for newly removed revisions as traffic target.
	for _, rev := range oldRevList.Items {
		if _, ok := revMap[rev.Name]; !ok {
			delete(rev.Labels, serving.RouteLabelKey)
			if _, err := revClient.Update(&rev); err != nil {
				logger.Errorf("Failed to remove route label from Revision %s: %s", rev.Name, err)
				return err
			}
		}
	}

	return nil
}

// computeRevisionRoutes computes RevisionRoute for a route object. If there is one or more inactive revisions and enableScaleToZero
// is true, a route rule with the activator service as the destination will be added. It returns the revision routes, the inactive
// revision name to which the activator should forward requests to, and error if there is any.
func (c *Controller) computeRevisionRoutes(
	ctx context.Context, route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration,
	revMap map[string]*v1alpha1.Revision) ([]RevisionRoute, string, error) {

	logger := logging.FromContext(ctx)
	logger.Debug("Figuring out routes")
	enableScaleToZero := c.enableScaleToZero.Get()
	// The inactive revision name which has the largest traffic weight.
	inactiveRev := ""
	// The max percent in all inactive revisions.
	maxInactivePercent := 0
	// The total percent of all inactive revisions.
	totalInactivePercent := 0
	ns := route.Namespace
	elaNS := controller.GetElaNamespaceName(ns)
	ret := []RevisionRoute{}

	for _, tt := range route.Spec.Traffic {
		var rev *v1alpha1.Revision
		var err error
		revName := tt.RevisionName
		if tt.ConfigurationName != "" {
			// Get the configuration's LatestReadyRevisionName
			revName = configMap[tt.ConfigurationName].Status.LatestReadyRevisionName
			if revName == "" {
				logger.Errorf("Configuration %s is not ready. Should skip updating route rules",
					tt.ConfigurationName)
				return nil, "", nil
			}
			// Revision has been already fetched indirectly in extendRevisionsWithIndirectTrafficTargets
			rev = revMap[revName]

		} else {
			// Direct revision has already been fetched
			rev = revMap[revName]
		}
		//TODO(grantr): What should happen if revisionName is empty?

		if rev == nil {
			// For safety, which should never happen.
			logger.Errorf("Failed to fetch Revision %s: %s", revName, err)
			return nil, "", err
		}

		revisionWeight := tt.Percent
		cond := rev.Status.GetCondition(v1alpha1.RevisionConditionReady)
		if enableScaleToZero && cond != nil {
			// A revision is considered inactive (yet) if it's in
			// "Inactive" condition, "Activating" or "Deactivating" condition.
			logger.Infof("cond: %v", cond)
			if (cond.Reason == "Inactive" && cond.Status == corev1.ConditionFalse) ||
				(cond.Reason == "Activating" && cond.Status == corev1.ConditionUnknown) ||
				(cond.Reason == "Deactivating" && cond.Status == corev1.ConditionFalse) {
				// Let inactiveRev be the Reserve revision with the largest traffic weight.
				if tt.Percent > maxInactivePercent {
					maxInactivePercent = tt.Percent
					inactiveRev = rev.Name
				}
				totalInactivePercent += tt.Percent
				revisionWeight = 0
			}
		}

		rr := RevisionRoute{
			Name:         tt.Name,
			RevisionName: rev.Name,
			Service:      rev.Status.ServiceName,
			Namespace:    elaNS,
			Weight:       revisionWeight,
		}
		logger.Infof("RevisionRoute: %v", rr)
		ret = append(ret, rr)
	}

	// TODO: The ideal solution is to append different revision name as headers for each inactive revision.
	// https://github.com/knative/serving/issues/882
	// TODO: We are adding activator to the route whether the weight is zero or positive
	// to workaround https://github.com/istio/istio/issues/5204
	// Once migration to Istio Gateway completes, we should change this back so that activator
	// is added to the list only if its weight is positive
	activatorRoute := RevisionRoute{
		Name:         controller.GetElaK8SActivatorServiceName(),
		RevisionName: inactiveRev,
		Service:      controller.GetElaK8SActivatorServiceName(),
		Namespace:    pkg.GetServingSystemNamespace(),
		Weight:       totalInactivePercent,
	}
	ret = append(ret, activatorRoute)
	return ret, inactiveRev, nil
}

// computeEmptyRevisionRoutes is a hack to work around https://github.com/istio/istio/issues/5204.
// Here we add empty/dummy route rules for non-target revisions to prepare to switch traffic to
// them in the future.  We are tracking this issue in https://github.com/knative/serving/issues/348.
//
// TODO:  Even though this fixes the 503s when switching revisions, revisions will have empty route
// rules to them for perpetuity, therefore not ideal.  We should remove this hack once
// https://github.com/istio/istio/issues/5204 is fixed, probably in 0.8.1.
func (c *Controller) computeEmptyRevisionRoutes(
	ctx context.Context, route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration,
	revMap map[string]*v1alpha1.Revision) ([]RevisionRoute, error) {
	logger := logging.FromContext(ctx)
	ns := route.Namespace
	elaNS := controller.GetElaNamespaceName(ns)
	revClient := c.ElaClientSet.ServingV1alpha1().Revisions(ns)
	revRoutes := []RevisionRoute{}
	for _, tt := range route.Spec.Traffic {
		configName := tt.ConfigurationName
		if configName != "" {
			// Get the configuration's LatestReadyRevisionName
			latestReadyRevName := configMap[configName].Status.LatestReadyRevisionName
			revs, err := revClient.List(metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", serving.ConfigurationLabelKey, configName),
			})
			if err != nil {
				logger.Errorf("Failed to fetch revisions of Configuration %q: %s", configName, err)
				return nil, err
			}
			for _, rev := range revs.Items {
				if _, ok := revMap[rev.Name]; !ok && rev.Name != latestReadyRevName {
					// This is a non-target revision.  Adding a dummy rule to make Istio happy,
					// so that if we switch traffic to them later we won't cause Istio to
					// throw spurious 503s. See https://github.com/istio/istio/issues/5204.
					revRoutes = append(revRoutes, RevisionRoute{
						RevisionName: rev.Name,
						Service:      rev.Status.ServiceName,
						Namespace:    elaNS,
						Weight:       0,
					})
				}
			}
		}
	}
	return revRoutes, nil
}

func (c *Controller) createOrUpdateRouteRules(ctx context.Context, route *v1alpha1.Route,
	configMap map[string]*v1alpha1.Configuration, revMap map[string]*v1alpha1.Revision) ([]RevisionRoute, string, error) {
	logger := logging.FromContext(ctx)
	// grab a client that's specific to RouteRule.
	ns := route.Namespace
	routeClient := c.ElaClientSet.ConfigV1alpha2().RouteRules(ns)
	if routeClient == nil {
		logger.Errorf("Failed to create resource client")
		return nil, "", fmt.Errorf("Couldn't get a routeClient")
	}

	revisionRoutes, inactiveRev, err := c.computeRevisionRoutes(ctx, route, configMap, revMap)
	if err != nil {
		logger.Errorf("Failed to get routes for %s : %q", route.Name, err)
		return nil, "", err
	}
	if len(revisionRoutes) == 0 {
		logger.Errorf("No routes were found for the service %q", route.Name)
		return nil, "", nil
	}

	// TODO: remove this once https://github.com/istio/istio/issues/5204 is fixed.
	emptyRoutes, err := c.computeEmptyRevisionRoutes(ctx, route, configMap, revMap)
	if err != nil {
		logger.Errorf("Failed to get empty routes for %s : %q", route.Name, err)
		return nil, "", err
	}
	revisionRoutes = append(revisionRoutes, emptyRoutes...)
	// Create route rule for the route domain
	routeRuleName := controller.GetRouteRuleName(route, nil)
	routeRules, err := routeClient.Get(routeRuleName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return nil, "", err
		}
		routeRules = MakeIstioRoutes(route, nil, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
		if _, err := routeClient.Create(routeRules); err != nil {
			c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create Istio route rule %q: %s", routeRules.Name, err)
			return nil, "", err
		}
		c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created Istio route rule %q", routeRules.Name)
	} else {
		routeRules.Spec = makeIstioRouteSpec(route, nil, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
		if _, err := routeClient.Update(routeRules); err != nil {
			c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed", "Failed to update Istio route rule %q: %s", routeRules.Name, err)
			return nil, "", err
		}
		c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated Istio route rule %q", routeRules.Name)
	}

	// Create route rule for named traffic targets
	for _, tt := range route.Spec.Traffic {
		if tt.Name == "" {
			continue
		}
		routeRuleName := controller.GetRouteRuleName(route, &tt)
		routeRules, err := routeClient.Get(routeRuleName, metav1.GetOptions{})
		if err != nil {
			if !apierrs.IsNotFound(err) {
				return nil, "", err
			}
			routeRules = MakeIstioRoutes(route, &tt, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
			if _, err := routeClient.Create(routeRules); err != nil {
				c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create Istio route rule %q: %s", routeRules.Name, err)
				return nil, "", err
			}
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created Istio route rule %q", routeRules.Name)
		} else {
			routeRules.Spec = makeIstioRouteSpec(route, &tt, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
			if _, err := routeClient.Update(routeRules); err != nil {
				return nil, "", err
			}
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated Istio route rule %q", routeRules.Name)
		}
	}
	if err := c.removeOutdatedRouteRules(ctx, route); err != nil {
		return nil, "", err
	}
	return revisionRoutes, inactiveRev, nil
}

func (c *Controller) updateStatus(ctx context.Context, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	logger := logging.FromContext(ctx)

	routeClient := c.ElaClientSet.ServingV1alpha1().Routes(route.Namespace)
	existing, err := routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		logger.Warn("Failed to update route status", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed", "Failed to get current status for route %q: %v", route.Name, err)
		return nil, err
	}

	// If there's nothing to update, just return.
	if reflect.DeepEqual(existing.Status, route.Status) {
		return existing, nil
	}

	existing.Status = route.Status
	// TODO: for CRD there's no updatestatus, so use normal update.
	updated, err := routeClient.Update(existing)
	if err != nil {
		logger.Warn("Failed to update route status", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for route %q: %v", route.Name, err)
		return nil, err
	}

	c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated status for route %q", route.Name)
	return updated, nil
}

// consolidateTrafficTargets will consolidate all duplicate revisions
// and configurations. If the traffic target names are unique, the traffic
// targets will not be consolidated.
func (c *Controller) consolidateTrafficTargets(ctx context.Context, route *v1alpha1.Route) {
	type trafficTarget struct {
		name              string
		revisionName      string
		configurationName string
	}

	logger := logging.FromContext(ctx)
	logger.Infof("Attempting to consolidate traffic targets")
	trafficTargets := route.Spec.Traffic
	trafficMap := make(map[trafficTarget]int)
	var order []trafficTarget

	for _, t := range trafficTargets {
		tt := trafficTarget{
			name:              t.Name,
			revisionName:      t.RevisionName,
			configurationName: t.ConfigurationName,
		}
		if trafficMap[tt] != 0 {
			logger.Infof(
				"Found duplicate traffic targets (name: %s, revision: %s, configuration:%s), consolidating traffic",
				tt.name,
				tt.revisionName,
				tt.configurationName,
			)
			trafficMap[tt] += t.Percent
		} else {
			trafficMap[tt] = t.Percent
			// The order to walk the map (Go randomizes otherwise)
			order = append(order, tt)
		}
	}

	consolidatedTraffic := []v1alpha1.TrafficTarget{}
	for _, tt := range order {
		p := trafficMap[tt]
		consolidatedTraffic = append(
			consolidatedTraffic,
			v1alpha1.TrafficTarget{
				Name:              tt.name,
				ConfigurationName: tt.configurationName,
				RevisionName:      tt.revisionName,
				Percent:           p,
			},
		)
	}
	route.Spec.Traffic = consolidatedTraffic
}

func (c *Controller) removeOutdatedRouteRules(ctx context.Context, u *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)
	ns := u.Namespace
	routeClient := c.ElaClientSet.ConfigV1alpha2().RouteRules(ns)
	if routeClient == nil {
		logger.Error("Failed to create resource client")
		return errors.New("Couldn't get a routeClient")
	}

	routeRuleList, err := routeClient.List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("route=%s", u.Name),
	})
	if err != nil {
		return err
	}

	routeRuleNames := map[string]struct{}{}
	routeRuleNames[controller.GetRouteRuleName(u, nil)] = struct{}{}
	for _, tt := range u.Spec.Traffic {
		if tt.Name == "" {
			continue
		}
		routeRuleNames[controller.GetRouteRuleName(u, &tt)] = struct{}{}
	}

	for _, r := range routeRuleList.Items {
		if _, ok := routeRuleNames[r.Name]; ok {
			continue
		}
		logger.Infof("Deleting outdated route: %s", r.Name)
		if err := routeClient.Delete(r.Name, nil); err != nil {
			if !apierrs.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) SyncConfiguration(config *v1alpha1.Configuration) {
	configName := config.Name
	ns := config.Namespace

	if config.Status.LatestReadyRevisionName == "" {
		c.Logger.Infof("Configuration %s is not ready", configName)
		return
	}

	routeName, ok := config.Labels[serving.RouteLabelKey]
	if !ok {
		c.Logger.Infof("Configuration %s does not have label %s", configName, serving.RouteLabelKey)
		return
	}

	logger := loggerWithRouteInfo(c.Logger, ns, routeName)
	ctx := logging.WithLogger(context.TODO(), logger)
	route, err := c.routeLister.Routes(ns).Get(routeName)
	if err != nil {
		logger.Error("Error fetching route upon configuration becoming ready", zap.Error(err))
		return
	}

	// Don't modify the informers copy
	route = route.DeepCopy()
	if _, err := c.syncTrafficTargetsAndUpdateRouteStatus(ctx, route); err != nil {
		logger.Error("Error updating route upon configuration becoming ready", zap.Error(err))
	}
}

func (c *Controller) SyncIngress(ingress *v1beta1.Ingress) {
	or := metav1.GetControllerOf(ingress)
	if or == nil || or.Kind != "Route" {
		// If ingress isn't owned by a route, no route update is required.
		return
	}
	routeName := or.Name

	if len(ingress.Status.LoadBalancer.Ingress) == 0 {
		// Route isn't ready if having no load-balancer ingress.
		return
	}
	for _, i := range ingress.Status.LoadBalancer.Ingress {
		if i.IP == "" {
			// Route isn't ready if some load balancer ingress doesn't have an IP.
			return
		}
	}
	ns := ingress.Namespace
	routeClient := c.ElaClientSet.ServingV1alpha1().Routes(ns)
	route, err := routeClient.Get(routeName, metav1.GetOptions{})
	if err != nil {
		c.Logger.Error(
			"Error fetching route upon ingress becoming",
			zap.Error(err),
			zap.String(logkey.Namespace, ns),
			zap.String(logkey.Route, routeName))
		return
	}
	if route.Status.IsReady() {
		return
	}
	route.Status.MarkIngressReady()

	logger := loggerWithRouteInfo(c.Logger, ns, routeName)
	ctx := logging.WithLogger(context.TODO(), logger)
	if _, err = c.updateStatus(ctx, route); err != nil {
		c.Logger.Error(
			"Error updating readiness of route upon ingress becoming",
			zap.Error(err),
			zap.String(logkey.Namespace, ns),
			zap.String(logkey.Route, routeName))
		return
	}
}

func (c *Controller) SyncConfigMap(configMap *corev1.ConfigMap) {
	if configMap.Namespace != pkg.GetServingSystemNamespace() || configMap.Name != controller.GetDomainConfigMapName() {
		return
	}

	c.Logger.Infof("Domain config map is added or updated: %v", configMap)
	newDomainConfig, err := NewDomainConfigFromConfigMap(configMap)
	if err != nil {
		c.Logger.Error("Failed to parse the new config map. Previous config map will be used.", zap.Error(err))
		return
	}
	c.setDomainConfig(newDomainConfig)
}
