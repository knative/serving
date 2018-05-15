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
	"errors"
	"fmt"
	"reflect"

	"github.com/golang/glog"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	listers "github.com/elafros/elafros/pkg/client/listers/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
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
// +controller:group=ela,version=v1alpha1,kind=Route,resource=routes
type Controller struct {
	*controller.ControllerBase

	// lister indexes properties about Route
	lister listers.RouteLister
	synced cache.InformerSynced

	// don't start the workers until configuration cache have been synced
	configSynced cache.InformerSynced

	// suffix used to construct Route domain.
	controllerConfig controller.Config

	// Autoscale enable scale to zero experiment flag.
	enableScaleToZero *k8sflag.BoolFlag
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// reconcileKey - function for mapping queue keys to resource names
//TODO(vaikas): somewhat generic (generic behavior)
func NewController(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config,
	enableScaleToZero *k8sflag.BoolFlag) controller.Interface {

	glog.Infof("Route controller Init")

	// obtain references to a shared index informer for the Routes and
	// Configurations type.
	informer := elaInformerFactory.Elafros().V1alpha1().Routes()
	configInformer := elaInformerFactory.Elafros().V1alpha1().Configurations()
	ingressInformer := kubeInformerFactory.Extensions().V1beta1().Ingresses()

	controller := &Controller{
		ControllerBase: controller.NewControllerBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, "Routes"),
		lister:            informer.Lister(),
		synced:            informer.Informer().HasSynced,
		configSynced:      configInformer.Informer().HasSynced,
		controllerConfig:  controllerConfig,
		enableScaleToZero: enableScaleToZero,
	}

	glog.Info("Setting up event handlers")
	configInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.addConfigurationEvent,
		UpdateFunc: controller.updateConfigurationEvent,
	})
	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.updateIngressEvent,
	})
	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, []cache.InformerSynced{c.synced, c.configSynced},
		c.updateRouteEvent, "Route")
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

	// Get the Route resource with this namespace/name
	route, err := c.lister.Routes(namespace).Get(name)

	// Don't modify the informers copy
	route = route.DeepCopy()

	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if apierrs.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("route %q in work queue no longer exists", key))
			return nil
		}

		return err
	}

	glog.Infof("Running reconcile Route for %s\n%+v\n", route.Name, route)

	// Create a placeholder service that is simply used by istio as a placeholder.
	// This service could eventually be the 'activator' service that will get all the
	// fallthrough traffic if there are no route rules (revisions to target).
	// This is one way to implement the 0->1. For now, we'll just create a placeholder
	// that selects nothing.
	glog.Infof("Creating/Updating placeholder k8s services")

	if err = c.reconcilePlaceholderService(route); err != nil {
		return err
	}

	// Call syncTrafficTargetsAndUpdateRouteStatus, which also updates the Route.Status
	// to contain the domain we will use for Ingress creation.

	if _, err = c.syncTrafficTargetsAndUpdateRouteStatus(route); err != nil {
		return err
	}

	// Then create or update the Ingress rule for this service
	glog.Infof("Creating or updating ingress rule")
	if err = c.reconcileIngress(route); err != nil {
		glog.Infof("Failed to create or update ingress rule: %s", err)
		return err
	}

	return nil
}

func (c *Controller) routeDomain(route *v1alpha1.Route) string {
	domain := c.controllerConfig.LookupDomainForLabels(route.ObjectMeta.Labels)
	return fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, domain)
}

// syncTrafficTargetsAndUpdateRouteStatus attempts to converge the actual state and desired state
// according to the traffic targets in Spec field for Route resource. It then updates the Status
// block of the Route and returns the updated one.
func (c *Controller) syncTrafficTargetsAndUpdateRouteStatus(route *v1alpha1.Route) (*v1alpha1.Route, error) {
	c.consolidateTrafficTargets(route)
	configMap, revMap, err := c.getDirectTrafficTargets(route)
	if err != nil {
		return nil, err
	}
	if err := c.extendConfigurationsWithIndirectTrafficTargets(route, configMap, revMap); err != nil {
		return nil, err
	}
	if err := c.extendRevisionsWithIndirectTrafficTargets(route, configMap, revMap); err != nil {
		return nil, err
	}

	if err := c.deleteLabelForOutsideOfGivenConfigurations(route, configMap); err != nil {
		return nil, err
	}
	if err := c.setLabelForGivenConfigurations(route, configMap); err != nil {
		return nil, err
	}

	if err := c.deleteLabelForOutsideOfGivenRevisions(route, revMap); err != nil {
		return nil, err
	}
	if err := c.setLabelForGivenRevisions(route, revMap); err != nil {
		return nil, err
	}

	// Then create the actual route rules.
	glog.Info("Creating Istio route rules")
	revisionRoutes, err := c.createOrUpdateRouteRules(route, configMap, revMap)
	if err != nil {
		glog.Infof("Failed to create Routes: %s", err)
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
	updated, err := c.updateStatus(route)
	if err != nil {
		glog.Warningf("Failed to update service status: %s", err)
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for route %q: %v", route.Name, err)
		return nil, err
	}
	c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated status for route %q", route.Name)
	return updated, nil
}

func (c *Controller) reconcilePlaceholderService(route *v1alpha1.Route) error {
	service := MakeRouteK8SService(route)
	if _, err := c.KubeClientSet.Core().Services(route.Namespace).Create(service); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// Service already exist.
			return nil
		}
		glog.Infof("Failed to create service: %s", err)
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create service %q: %v", service.Name, err)
		return err
	}
	glog.Infof("Created service: %q", service.Name)
	c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created service %q", service.Name)
	return nil
}

func (c *Controller) reconcileIngress(route *v1alpha1.Route) error {
	ingressNamespace := route.Namespace
	ingressName := controller.GetElaK8SIngressName(route)
	ingress := MakeRouteIngress(route)
	ingressClient := c.KubeClientSet.Extensions().Ingresses(ingressNamespace)
	existing, err := ingressClient.Get(ingressName, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			if _, err = ingressClient.Create(ingress); err == nil {
				glog.Infof("Created ingress %q in namespace %q", ingressName, ingressNamespace)
				c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created Ingress %q in namespace %q", ingressName, ingressNamespace)
			}
		}
		return err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(existing.Spec, ingress.Spec) {
		existing.Spec = ingress.Spec
		if _, err = ingressClient.Update(existing); err == nil {
			glog.Infof("Updated ingress %q in namespace %q", ingressName, ingressNamespace)
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated Ingress %q in namespace %q", ingressName, ingressNamespace)
		}
		return err
	}
	return nil
}

func (c *Controller) getDirectTrafficTargets(route *v1alpha1.Route) (
	map[string]*v1alpha1.Configuration, map[string]*v1alpha1.Revision, error) {
	ns := route.Namespace
	configClient := c.ElaClientSet.ElafrosV1alpha1().Configurations(ns)
	revClient := c.ElaClientSet.ElafrosV1alpha1().Revisions(ns)
	configMap := map[string]*v1alpha1.Configuration{}
	revMap := map[string]*v1alpha1.Revision{}

	for _, tt := range route.Spec.Traffic {
		if tt.ConfigurationName != "" {
			configName := tt.ConfigurationName
			config, err := configClient.Get(configName, metav1.GetOptions{})
			if err != nil {
				glog.Infof("Failed to fetch Configuration %q: %v", configName, err)
				return nil, nil, err
			}
			configMap[configName] = config
		} else {
			revName := tt.RevisionName
			rev, err := revClient.Get(revName, metav1.GetOptions{})
			if err != nil {
				glog.Infof("Failed to fetch Revision %q: %v", revName, err)
				return nil, nil, err
			}
			revMap[revName] = rev
		}
	}

	return configMap, revMap, nil
}

func (c *Controller) extendConfigurationsWithIndirectTrafficTargets(
	route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration, revMap map[string]*v1alpha1.Revision) error {
	ns := route.Namespace
	configClient := c.ElaClientSet.ElafrosV1alpha1().Configurations(ns)

	// Get indirect configurations.
	for _, rev := range revMap {
		if configName, ok := rev.Labels[ela.ConfigurationLabelKey]; ok {
			if _, ok := configMap[configName]; !ok {
				// This is not a duplicated configuration
				config, err := configClient.Get(configName, metav1.GetOptions{})
				if err != nil {
					glog.Errorf("Failed to fetch Configuration %s: %s", configName, err)
					return err
				}
				configMap[configName] = config
			}
		} else {
			glog.Warningf("Revision %s does not have label %s", rev.Name, ela.ConfigurationLabelKey)
		}
	}

	return nil
}

func (c *Controller) extendRevisionsWithIndirectTrafficTargets(
	route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration, revMap map[string]*v1alpha1.Revision) error {
	ns := route.Namespace
	revisionClient := c.ElaClientSet.ElafrosV1alpha1().Revisions(ns)

	for _, tt := range route.Spec.Traffic {
		if tt.ConfigurationName != "" {
			configName := tt.ConfigurationName
			if config, ok := configMap[configName]; ok {

				revName := config.Status.LatestReadyRevisionName
				if revName == "" {
					glog.Infof("Configuration %s is not ready. Skipping Configuration %q during route reconcile",
						tt.ConfigurationName)
					continue
				}
				// Check if it is a duplicated revision
				if _, ok := revMap[revName]; !ok {
					rev, err := revisionClient.Get(revName, metav1.GetOptions{})
					if err != nil {
						glog.Errorf("Failed to fetch Revision %s: %s", revName, err)
						return err
					}
					revMap[revName] = rev
				}
			}
		}
	}

	return nil
}

func (c *Controller) setLabelForGivenConfigurations(
	route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration) error {
	configClient := c.ElaClientSet.ElafrosV1alpha1().Configurations(route.Namespace)

	// Validate
	for _, config := range configMap {
		if routeName, ok := config.Labels[ela.RouteLabelKey]; ok {
			// TODO(yanweiguo): add a condition in status for this error
			if routeName != route.Name {
				errMsg := fmt.Sprintf("Configuration %q is already in use by %q, and cannot be used by %q",
					config.Name, routeName, route.Name)
				c.Recorder.Event(route, corev1.EventTypeWarning, "ConfigurationInUse", errMsg)
				return errors.New(errMsg)
			}
		}
	}

	// Set label for newly added configurations as traffic target.
	for _, config := range configMap {
		if config.Labels == nil {
			config.Labels = make(map[string]string)
		} else if _, ok := config.Labels[ela.RouteLabelKey]; ok {
			continue
		}
		config.Labels[ela.RouteLabelKey] = route.Name
		if _, err := configClient.Update(config); err != nil {
			glog.Errorf("Failed to update Configuration %s: %s", config.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) setLabelForGivenRevisions(
	route *v1alpha1.Route, revMap map[string]*v1alpha1.Revision) error {
	revisionClient := c.ElaClientSet.ElafrosV1alpha1().Revisions(route.Namespace)

	// Validate revision if it already has a route label
	for _, rev := range revMap {
		if routeName, ok := rev.Labels[ela.RouteLabelKey]; ok {
			if routeName != route.Name {
				errMsg := fmt.Sprintf("Revision %q is already in use by %q, and cannot be used by %q",
					rev.Name, routeName, route.Name)
				c.Recorder.Event(route, corev1.EventTypeWarning, "RevisionInUse", errMsg)
				return errors.New(errMsg)
			}
		}
	}

	for _, rev := range revMap {
		if rev.Labels == nil {
			rev.Labels = make(map[string]string)
		} else if _, ok := rev.Labels[ela.RouteLabelKey]; ok {
			continue
		}
		rev.Labels[ela.RouteLabelKey] = route.Name
		if _, err := revisionClient.Update(rev); err != nil {
			glog.Errorf("Failed to add route label to Revision %s: %s", rev.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) deleteLabelForOutsideOfGivenConfigurations(
	route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration) error {
	configClient := c.ElaClientSet.ElafrosV1alpha1().Configurations(route.Namespace)
	// Get Configurations set as traffic target before this sync.
	oldConfigsList, err := configClient.List(
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", ela.RouteLabelKey, route.Name),
		},
	)
	if err != nil {
		glog.Errorf("Failed to fetch configurations with label '%s=%s': %s",
			ela.RouteLabelKey, route.Name, err)
		return err
	}

	// Delete label for newly removed configurations as traffic target.
	for _, config := range oldConfigsList.Items {
		if _, ok := configMap[config.Name]; !ok {
			delete(config.Labels, ela.RouteLabelKey)
			if _, err := configClient.Update(&config); err != nil {
				glog.Errorf("Failed to update Configuration %s: %s", config.Name, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) deleteLabelForOutsideOfGivenRevisions(
	route *v1alpha1.Route, revMap map[string]*v1alpha1.Revision) error {
	revClient := c.ElaClientSet.ElafrosV1alpha1().Revisions(route.Namespace)

	oldRevList, err := revClient.List(
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", ela.RouteLabelKey, route.Name),
		},
	)
	if err != nil {
		glog.Errorf("Failed to fetch revisions with label '%s=%s': %s",
			ela.RouteLabelKey, route.Name, err)
		return err
	}

	// Delete label for newly removed revisions as traffic target.
	for _, rev := range oldRevList.Items {
		if _, ok := revMap[rev.Name]; !ok {
			delete(rev.Labels, ela.RouteLabelKey)
			if _, err := revClient.Update(&rev); err != nil {
				glog.Errorf("Failed to remove route label from Revision %s: %s", rev.Name, err)
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
	route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration, revMap map[string]*v1alpha1.Revision) ([]RevisionRoute, string, error) {
	glog.V(4).Infof("Figuring out routes for Route: %s", route.Name)
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
				glog.Errorf("Configuration %s is not ready. Should skip updating route rules",
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
			glog.Errorf("Failed to fetch Revision %s: %s", revName, err)
			return nil, "", err
		}

		hasRouteRule := true
		cond := rev.Status.GetCondition(v1alpha1.RevisionConditionReady)
		if enableScaleToZero && cond != nil {
			// A revision is considered inactive (yet) if it's in
			// "Inactive" condition or "Activating" condition.
			if (cond.Reason == "Inactive" && cond.Status == corev1.ConditionFalse) ||
				(cond.Reason == "Activating" && cond.Status == corev1.ConditionUnknown) {
				// Let inactiveRev be the Reserve revision with the largest traffic weight.
				if tt.Percent > maxInactivePercent {
					maxInactivePercent = tt.Percent
					inactiveRev = rev.Name
				}
				totalInactivePercent += tt.Percent
				hasRouteRule = false
			}
		}

		if hasRouteRule {
			rr := RevisionRoute{
				Name:         tt.Name,
				RevisionName: rev.Name,
				Service:      rev.Status.ServiceName,
				Namespace:    elaNS,
				Weight:       tt.Percent,
			}
			ret = append(ret, rr)
		}
	}

	// TODO: The ideal solution is to append different revision name as headers for each inactive revision.
	// https://github.com/elafros/elafros/issues/882
	if totalInactivePercent > 0 {
		activatorRoute := RevisionRoute{
			Name:         controller.GetElaK8SActivatorServiceName(),
			RevisionName: inactiveRev,
			Service:      controller.GetElaK8SActivatorServiceName(),
			Namespace:    controller.GetElaK8SActivatorNamespace(),
			Weight:       totalInactivePercent,
		}
		ret = append(ret, activatorRoute)
	}
	return ret, inactiveRev, nil
}

// computeEmptyRevisionRoutes is a hack to work around https://github.com/istio/istio/issues/5204.
// Here we add empty/dummy route rules for non-target revisions to prepare to switch traffic to
// them in the future.  We are tracking this issue in https://github.com/elafros/elafros/issues/348.
//
// TODO:  Even though this fixes the 503s when switching revisions, revisions will have empty route
// rules to them for perpetuity, therefore not ideal.  We should remove this hack once
// https://github.com/istio/istio/issues/5204 is fixed, probably in 0.8.1.
func (c *Controller) computeEmptyRevisionRoutes(
	route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration, revMap map[string]*v1alpha1.Revision) ([]RevisionRoute, error) {
	ns := route.Namespace
	elaNS := controller.GetElaNamespaceName(ns)
	revClient := c.ElaClientSet.ElafrosV1alpha1().Revisions(ns)
	revRoutes := []RevisionRoute{}
	for _, tt := range route.Spec.Traffic {
		configName := tt.ConfigurationName
		if configName != "" {
			// Get the configuration's LatestReadyRevisionName
			latestReadyRevName := configMap[configName].Status.LatestReadyRevisionName
			revs, err := revClient.List(metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", ela.ConfigurationLabelKey, configName),
			})
			if err != nil {
				glog.Errorf("Failed to fetch revisions of Configuration %q: %s", configName, err)
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

func (c *Controller) createOrUpdateRouteRules(route *v1alpha1.Route, configMap map[string]*v1alpha1.Configuration,
	revMap map[string]*v1alpha1.Revision) ([]RevisionRoute, error) {
	// grab a client that's specific to RouteRule.
	ns := route.Namespace
	routeClient := c.ElaClientSet.ConfigV1alpha2().RouteRules(ns)
	if routeClient == nil {
		glog.Errorf("Failed to create resource client")
		return nil, fmt.Errorf("Couldn't get a routeClient")
	}

	revisionRoutes, inactiveRev, err := c.computeRevisionRoutes(route, configMap, revMap)
	if err != nil {
		glog.Errorf("Failed to get routes for %s : %q", route.Name, err)
		return nil, err
	}
	if len(revisionRoutes) == 0 {
		glog.Errorf("No routes were found for the service %q", route.Name)
		return nil, nil
	}

	// TODO: remove this once https://github.com/istio/istio/issues/5204 is fixed.
	emptyRoutes, err := c.computeEmptyRevisionRoutes(route, configMap, revMap)
	if err != nil {
		glog.Errorf("Failed to get empty routes for %s : %q", route.Name, err)
		return nil, err
	}
	revisionRoutes = append(revisionRoutes, emptyRoutes...)
	// Create route rule for the route domain
	routeRuleName := controller.GetRouteRuleName(route, nil)
	routeRules, err := routeClient.Get(routeRuleName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return nil, err
		}
		routeRules = MakeIstioRoutes(route, nil, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
		if _, err := routeClient.Create(routeRules); err != nil {
			c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create Istio route rule %q: %s", routeRules.Name, err)
			return nil, err
		}
		c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created Istio route rule %q", routeRules.Name)
	} else {
		routeRules.Spec = makeIstioRouteSpec(route, nil, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
		if _, err := routeClient.Update(routeRules); err != nil {
			c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed", "Failed to update Istio route rule %q: %s", routeRules.Name, err)
			return nil, err
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
				return nil, err
			}
			routeRules = MakeIstioRoutes(route, &tt, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
			if _, err := routeClient.Create(routeRules); err != nil {
				c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create Istio route rule %q: %s", routeRules.Name, err)
				return nil, err
			}
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created Istio route rule %q", routeRules.Name)
		} else {
			routeRules.Spec = makeIstioRouteSpec(route, &tt, ns, revisionRoutes, c.routeDomain(route), inactiveRev)
			if _, err := routeClient.Update(routeRules); err != nil {
				return nil, err
			}
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated Istio route rule %q", routeRules.Name)
		}
	}
	if err := c.removeOutdatedRouteRules(route); err != nil {
		return nil, err
	}
	return revisionRoutes, nil
}

func (c *Controller) updateStatus(route *v1alpha1.Route) (*v1alpha1.Route, error) {
	routeClient := c.ElaClientSet.ElafrosV1alpha1().Routes(route.Namespace)
	existing, err := routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(existing.Status, route.Status) {
		existing.Status = route.Status
		// TODO: for CRD there's no updatestatus, so use normal update.
		return routeClient.Update(existing)
	}
	return existing, nil
}

// consolidateTrafficTargets will consolidate all duplicate revisions
// and configurations. If the traffic target names are unique, the traffic
// targets will not be consolidated.
func (c *Controller) consolidateTrafficTargets(route *v1alpha1.Route) {
	type trafficTarget struct {
		name              string
		revisionName      string
		configurationName string
	}

	glog.Infof("Attempting to consolidate traffic targets")
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
			glog.Infof(
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

func (c *Controller) removeOutdatedRouteRules(u *v1alpha1.Route) error {
	ns := u.Namespace
	routeClient := c.ElaClientSet.ConfigV1alpha2().RouteRules(ns)
	if routeClient == nil {
		glog.Error("Failed to create resource client")
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
		glog.Info("Deleting outdated route: %s", r.Name)
		if err := routeClient.Delete(r.Name, nil); err != nil {
			if !apierrs.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) addConfigurationEvent(obj interface{}) {
	config := obj.(*v1alpha1.Configuration)
	configName := config.Name
	ns := config.Namespace

	if config.Status.LatestReadyRevisionName == "" {
		glog.Infof("Configuration %s is not ready", configName)
		return
	}

	routeName, ok := config.Labels[ela.RouteLabelKey]
	if !ok {
		glog.Warningf("Configuration %s does not have label %s",
			configName, ela.RouteLabelKey)
		return
	}

	route, err := c.lister.Routes(ns).Get(routeName)
	if err != nil {
		glog.Errorf("Error fetching route '%s/%s' upon configuration becoming ready: %v",
			ns, routeName, err)
		return
	}

	// Don't modify the informers copy
	route = route.DeepCopy()
	if _, err := c.syncTrafficTargetsAndUpdateRouteStatus(route); err != nil {
		glog.Errorf("Error updating route '%s/%s' upon configuration becoming ready: %v",
			ns, routeName, err)
	}
}

func (c *Controller) updateConfigurationEvent(old, new interface{}) {
	c.addConfigurationEvent(new)
}

func (c *Controller) updateIngressEvent(old, new interface{}) {
	ingress := new.(*v1beta1.Ingress)
	// If ingress isn't owned by a route, no route update is required.
	routeName := controller.LookupOwningRouteName(ingress.OwnerReferences)
	if routeName == "" {
		return
	}
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
	routeClient := c.ElaClientSet.ElafrosV1alpha1().Routes(ns)
	route, err := routeClient.Get(routeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Error fetching route '%s/%s' upon ingress becoming: %v",
			ns, routeName, err)
		return
	}
	if route.Status.IsReady() {
		return
	}
	// Mark route as ready.
	route.Status.SetCondition(&v1alpha1.RouteCondition{
		Type:   v1alpha1.RouteConditionReady,
		Status: corev1.ConditionTrue,
	})

	if _, err = routeClient.Update(route); err != nil {
		glog.Errorf("Error updating readiness of route '%s/%s' upon ingress becoming: %v",
			ns, routeName, err)
		return
	}
}
