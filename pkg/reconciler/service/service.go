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
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	"strconv"

	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	ksvcreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/service"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmp"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	configresources "knative.dev/serving/pkg/reconciler/configuration/resources"
	"knative.dev/serving/pkg/reconciler/service/resources"
	resourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
)

// Reconciler implements controller.Reconciler for Service resources.
type Reconciler struct {
	client clientset.Interface

	// listers index properties about resources
	configurationLister       listers.ConfigurationLister
	revisionLister            listers.RevisionLister
	routeLister               listers.RouteLister
	serviceOrchestratorLister listers.ServiceOrchestratorLister
	podAutoscalerLister       palisters.PodAutoscalerLister
}

// Check that our Reconciler implements ksvcreconciler.Interface
var _ ksvcreconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, service *v1.Service) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
	defer cancel()

	logger := logging.FromContext(ctx)
	config, err := c.config(ctx, service)
	if err != nil {
		return err
	}

	// If the size of the traffic list is 0 or 1, there will be only one revision accepting the traffic.
	_, err = c.serviceOrchestrator(ctx, service, config)
	if err != nil {
		return err
	}

	if config.Generation != config.Status.ObservedGeneration {
		// The Configuration hasn't yet reconciled our latest changes to
		// its desired state, so its conditions are outdated.
		service.Status.MarkConfigurationNotReconciled()

		// If BYO-Revision name is used we must serialize reconciling the Configuration
		// and Route. Wait for observed generation to match before continuing.
		if config.Spec.GetTemplate().Name != "" {
			return nil
		}
	} else {
		logger.Debugf("Configuration Conditions = %#v", config.Status.Conditions)
		// Update our Status based on the state of our underlying Configuration.
		service.Status.PropagateConfigurationStatus(&config.Status)
	}

	// When the Configuration names a Revision, check that the named Revision is owned
	// by our Configuration and matches its generation before reprogramming the Route,
	// otherwise a bad patch could lead to folks inadvertently routing traffic to a
	// pre-existing Revision (possibly for another Configuration).
	if err := CheckNameAvailability(config, c.revisionLister); err != nil &&
		!apierrs.IsNotFound(err) {
		logger.Errorf("I did not get an equal generation and revision label!!!!!!!!!!!!!!!\n")
		logger.Errorf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!\n")
		service.Status.MarkRevisionNameTaken(config.Spec.GetTemplate().Name)
		return nil
	}

	logger.Info("I have got an equal generation and revision label\n")
	r, _ := c.latestCreatedRevision(config)
	if r != nil {
		logger.Info(r.Name)
	}

	logger.Info("HAAAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAAHAAAAAAA\n")
	route, err := c.route(ctx, service)
	if err != nil {
		return err
	}

	// Update our Status based on the state of our underlying Route.
	ss := &service.Status
	if route.Generation != route.Status.ObservedGeneration {
		// The Route hasn't yet reconciled our latest changes to
		// its desired state, so its conditions are outdated.
		ss.MarkRouteNotReconciled()
	} else {
		// Update our Status based on the state of our underlying Route.
		ss.PropagateRouteStatus(&route.Status)
	}

	c.checkRoutesNotReady(config, logger, route, service)

	rn, _ := c.latestCreatedRevision(config)
	if rn != nil {
		logger.Info(rn.Name)
		//logger.Info(*rn.Status.ActualReplicas)
		//if rn.Status.DesiredReplicas != nil {
		//	logger.Info(*rn.Status.DesiredReplicas)
		//}

	} else {
		logger.Info("did not get latest revision")
	}

	ro, _, _ := c.previousCreatedRevision(config)
	if ro != nil {
		logger.Info(ro.Name)
		//logger.Info(*ro.Status.ActualReplicas)
		//if ro.Status.DesiredReplicas != nil {
		//	logger.Info(*ro.Status.DesiredReplicas)
		//}
	} else {
		logger.Info("did not get previous revision")
	}
	logger.Info("HAAAAArouteHAAAAArouteHAAAAArouteHAAAAArouteHAAAAArouteHAAAAArouteHAAAAArouteHAAAAArouteHAAAAArouteHAAAAAroute\n")

	return nil
}

func (c *Reconciler) latestCreatedRevision(config *v1.Configuration) (*v1.Revision, error) {
	lister := c.revisionLister.Revisions(config.Namespace)
	// Even though we now name revisions consistently and could fetch by name, we have to
	// keep this code to stay functional for older revisions that predate that change.
	generationKey := serving.ConfigurationGenerationLabelKey
	list, err := lister.List(labels.SelectorFromSet(labels.Set{
		generationKey:                 configresources.RevisionLabelValueForKey(generationKey, config),
		serving.ConfigurationLabelKey: config.Name,
	}))
	if err == nil && len(list) > 0 {
		return list[0], nil
	}
	return nil, err
}

func (c *Reconciler) previousCreatedRevision(config *v1.Configuration) (*v1.Revision, bool, error) {

	lister := c.revisionLister.Revisions(config.Namespace)

	// Even though we now name revisions consistently and could fetch by name, we have to
	// keep this code to stay functional for older revisions that predate that change.
	generationKey := serving.ConfigurationGenerationLabelKey
	list, err := lister.List(labels.SelectorFromSet(labels.Set{
		generationKey:                 configresources.RevisionLabelValueForKey(generationKey, config),
		serving.ConfigurationLabelKey: config.Name,
	}))

	if err == nil && len(list) > 0 {
		return list[len(list)-1], false, nil
	}

	return nil, false, nil
}

func (c *Reconciler) serviceOrchestrator(ctx context.Context, service *v1.Service, config *v1.Configuration) (*v1.ServiceOrchestrator, error) {
	logger := logging.FromContext(ctx)
	logger.Infof("call serviceOrchestrator call serviceOrchestrator call serviceOrchestrator call serviceOrchestrator call serviceOrchestrator call serviceOrchestrator call serviceOrchestrator")

	recorder := controller.GetEventRecorder(ctx)

	routeName := resourcenames.Route(service)
	route, err := c.routeLister.Routes(service.Namespace).Get(routeName)
	if apierrs.IsNotFound(err) {
		route = nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get the route: %w", err)
	}

	soName := resourcenames.ServiceOrchestrator(service)
	so, err := c.serviceOrchestratorLister.ServiceOrchestrators(config.Namespace).Get(soName)
	if apierrs.IsNotFound(err) {
		so, err = c.createServiceOrchestrator(ctx, service, config, route)
		if err != nil {
			recorder.Eventf(service, corev1.EventTypeWarning, "CreationFailed", "Failed to create ServiceOrchestrator %q: %v", soName, err)
			return nil, fmt.Errorf("failed to create ServiceOrchestrator: %w", err)
		}
		recorder.Eventf(service, corev1.EventTypeNormal, "Created", "Created ServiceOrchestrator %q", soName)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get Configuration: %w", err)
	} else if !metav1.IsControlledBy(so, service) {
		// Surface an error in the service's status,and return an error.
		service.Status.MarkServiceOrchestratorNotOwned(soName)
		return nil, fmt.Errorf("service: %q does not own ServiceOrchestrator: %q", service.Name, soName)
	} else if so, err = c.reconcileServiceOrchestrator(ctx, service, config, so); err != nil {
		return nil, fmt.Errorf("failed to reconcile Configuration: %w", err)
	}

	return nil, nil
}

func (c *Reconciler) config(ctx context.Context, service *v1.Service) (*v1.Configuration, error) {
	recorder := controller.GetEventRecorder(ctx)
	configName := resourcenames.Configuration(service)
	config, err := c.configurationLister.Configurations(service.Namespace).Get(configName)
	if apierrs.IsNotFound(err) {
		config, err = c.createConfiguration(ctx, service)
		if err != nil {
			recorder.Eventf(service, corev1.EventTypeWarning, "CreationFailed", "Failed to create Configuration %q: %v", configName, err)
			return nil, fmt.Errorf("failed to create Configuration: %w", err)
		}
		recorder.Eventf(service, corev1.EventTypeNormal, "Created", "Created Configuration %q", configName)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get Configuration: %w", err)
	} else if !metav1.IsControlledBy(config, service) {
		// Surface an error in the service's status,and return an error.
		service.Status.MarkConfigurationNotOwned(configName)
		return nil, fmt.Errorf("service: %q does not own configuration: %q", service.Name, configName)
	} else if config, err = c.reconcileConfiguration(ctx, service, config); err != nil {
		return nil, fmt.Errorf("failed to reconcile Configuration: %w", err)
	}
	return config, nil
}

func (c *Reconciler) route(ctx context.Context, service *v1.Service) (*v1.Route, error) {
	recorder := controller.GetEventRecorder(ctx)
	routeName := resourcenames.Route(service)
	route, err := c.routeLister.Routes(service.Namespace).Get(routeName)
	if apierrs.IsNotFound(err) {
		route, err = c.createRoute(ctx, service)
		if err != nil {
			recorder.Eventf(service, corev1.EventTypeWarning, "CreationFailed", "Failed to create Route %q: %v", routeName, err)
			return nil, fmt.Errorf("failed to create Route: %w", err)
		}
		recorder.Eventf(service, corev1.EventTypeNormal, "Created", "Created Route %q", routeName)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get Route: %w", err)
	} else if !metav1.IsControlledBy(route, service) {
		// Surface an error in the service's status, and return an error.
		service.Status.MarkRouteNotOwned(routeName)
		return nil, fmt.Errorf("service: %q does not own route: %q", service.Name, routeName)
	} else if route, err = c.reconcileRoute(ctx, service, route); err != nil {
		return nil, fmt.Errorf("failed to reconcile Route: %w", err)
	}
	return route, nil
}

func (c *Reconciler) checkRoutesNotReady(config *v1.Configuration, logger *zap.SugaredLogger, route *v1.Route, service *v1.Service) {
	// `manual` is not reconciled.
	rc := service.Status.GetCondition(v1.ServiceConditionRoutesReady)
	if rc == nil || rc.Status != corev1.ConditionTrue {
		return
	}

	if len(route.Spec.Traffic) != len(route.Status.Traffic) {
		service.Status.MarkRouteNotYetReady()
		logger.Errorf("No good route No good route No good route No good route No good route No good route No good route No good route")
		return
	}

	want, got := route.Spec.DeepCopy().Traffic, route.Status.DeepCopy().Traffic
	// Replace `configuration` target with its latest ready revision.
	for idx := range want {
		if want[idx].ConfigurationName == config.Name {
			want[idx].RevisionName = config.Status.LatestReadyRevisionName
			want[idx].ConfigurationName = ""
		}
	}
	ignoreFields := cmpopts.IgnoreFields(v1.TrafficTarget{}, "URL", "LatestRevision")
	if diff, err := kmp.SafeDiff(got, want, ignoreFields); err != nil || diff != "" {
		logger.Errorf("Route %s is not yet what we want: %s", route.Name, diff)
		logger.Errorf("No good route what we need No good route what we need No good route what we need No good route what we need")
		service.Status.MarkRouteNotYetReady()
	}
}

func (c *Reconciler) createConfiguration(ctx context.Context, service *v1.Service) (*v1.Configuration, error) {
	return c.client.ServingV1().Configurations(service.Namespace).Create(
		ctx, resources.MakeConfiguration(service), metav1.CreateOptions{})
}

func (c *Reconciler) calculateStageRevisionTarget(ctx context.Context, so *v1.ServiceOrchestrator) (*v1.ServiceOrchestrator, error) {
	logger := logging.FromContext(ctx)
	logger.Info("calculateStageRevisionTarget calculateStageRevisionTarget calculateStageRevisionTarget calculateStageRevisionTarget")

	if so.Status.StageRevisionStatus == nil || len(so.Status.StageRevisionStatus) == 0 {
		logger.Info("I do not see StageRevisionStatus in status.")

		// There is no stage revision status, which indicates that no route is configured. We can directly set
		// the ultimate revision target as the current stage revision target.
		so.Spec.StageRevisionTarget = append([]v1.RevisionTarget{}, so.Spec.RevisionTarget...)

	} else {

		logger.Info("I see StageRevisionStatus in status.")

		if len(so.Spec.InitialRevisionStatus) > 1 || len(so.Spec.RevisionTarget) > 1 {
			// If the initial revision status contains more than one revision, or the ultimate revision target contains
			// more than one revision, we will set the current stage target to the ultimate revision target.
			so.Spec.StageRevisionTarget = append([]v1.RevisionTarget{}, so.Spec.RevisionTarget...)
		} else {
			// If the initial revision status and ultimate revision target both contains only one revision, we will
			// roll out the revision incrementally.
			// Check if stage revision status is ready or in progress
			if so.IsStageReady() {
				if so.IsReady() {
					// If the last stage has rolled out, nothing changes.
					return so, nil
				} else {
					// The current stage revision is complete. We need to calculate the next stage target.
					so = c.updateStageRevisionSpec(so)
				}
			} else if so.IsStageInProgress() {
				// Do nothing, because it is in progress to the current so.Spec.StageRevisionTarget
				return so, nil
			}
		}
	}

	return so, nil
}

func (c *Reconciler) updateStageRevisionSpec(so *v1.ServiceOrchestrator) *v1.ServiceOrchestrator {
	// Based on the list of revisions in the stage revision status and the ultimate revision target,
	// we can get the current number of replicas based on the kpa for each revision, because the kpa shares
	// the same name as the revision.

	// Based on the oversubscription ratio, the stage revision status, the number of replicas of the new revision,
	// we can estimate the number of replicas of the next stage. If the number of replicas of the old revision takes
	// 100% of the traffic, we need to use the number of replicas for the old revision.

	//for _, revision := range so.Status.StageRevisionStatus {
	//
	//}

	return so
}

func (c *Reconciler) createServiceOrchestrator(ctx context.Context, service *v1.Service, config *v1.Configuration,
	route *v1.Route) (*v1.ServiceOrchestrator, error) {
	// To create the service, orchestrator, we need to make sure we have stageTraffic and Traffic in the spec, and
	// stageReady, and Ready in the status.
	records := map[string]resources.RevisionRecord{}

	lister := c.podAutoscalerLister.PodAutoscalers(config.Namespace)
	list, err := lister.List(labels.SelectorFromSet(labels.Set{
		serving.ConfigurationLabelKey: config.Name,
	}))

	logger := logging.FromContext(ctx)
	logger.Infof("Test service orchestrator Test service orchestrator Test service orchestrator Test service orchestrator")

	if err == nil && len(list) > 0 {
		for _, revision := range list {
			logger.Infof("List the revision List the revision List the revision List the revision List the revision List the revision")
			logger.Info(revision)

			logger.Infof("check the annotations")
			logger.Info(revision.Annotations)
			record := resources.RevisionRecord{}
			//if kpa.Status.DesiredScale != nil {
			//	*record.Replicas = *kpa.Status.DesiredScale
			//}

			if val, ok := revision.Annotations[autoscaling.MinScaleAnnotationKey]; ok {
				logger.Infof("check the annotations min ok")

				i, err := strconv.ParseInt(val, 10, 32)
				if err == nil {
					logger.Infof("check the annotations min val set")
					record.MinScale = ptr.Int32(int32(i))
				} else {
					logger.Infof("check the annotations min val not set")
					logger.Info(err)
				}
			}

			if val, ok := revision.Annotations[autoscaling.MaxScaleAnnotationKey]; ok {
				logger.Infof("check the max annotations ok")
				i, err := strconv.ParseInt(val, 10, 32)
				if err == nil {
					record.MaxScale = ptr.Int32(int32(i))
				}
			}
			record.Name = revision.Name
			records[revision.Name] = record

			logger.Infof("print records")
			logger.Info(record)
		}
	} else {
		return nil, fmt.Errorf("failed to get the kpa: %w", err)
	}

	logger.Infof("Good Route!!!!!!!!!Good Route!!!!!Good Route!!!!!Good Route!!!!!Good Route!!!!!Good Route!!!!!Good Route!!!!!")

	so := resources.MakeServiceOrchestrator(service, config, route, records, logger)
	so, err = c.client.ServingV1().ServiceOrchestrators(service.Namespace).Create(
		ctx, so, metav1.CreateOptions{})
	if err != nil {
		return so, err
	}
	so, _ = c.calculateStageRevisionTarget(ctx, so)
	//origin := so.DeepCopy()
	//if equality.Semantic.DeepEqual(origin.Spec, so.Spec) {
	//	return so, nil
	//}
	logger.Infof("check the StageRevisionTarget")
	logger.Info(so.Spec.StageRevisionTarget)
	so, err = c.client.ServingV1().ServiceOrchestrators(service.Namespace).Update(ctx, so, metav1.UpdateOptions{})
	logger.Infof("check the error")
	logger.Info(err)
	return so, err
}

func configSemanticEquals(ctx context.Context, desiredConfig, config *v1.Configuration) (bool, error) {
	logger := logging.FromContext(ctx)
	specDiff, err := kmp.SafeDiff(desiredConfig.Spec, config.Spec)
	if err != nil {
		logger.Warnw("Error diffing config spec", zap.Error(err))
		return false, fmt.Errorf("failed to diff Configuration: %w", err)
	} else if specDiff != "" {
		logger.Info("Reconciling configuration diff (-desired, +observed):\n", specDiff)
	}
	return equality.Semantic.DeepEqual(desiredConfig.Spec, config.Spec) &&
		equality.Semantic.DeepEqual(desiredConfig.Labels, config.Labels) &&
		equality.Semantic.DeepEqual(desiredConfig.Annotations, config.Annotations) &&
		specDiff == "", nil
}

func (c *Reconciler) reconcileServiceOrchestrator(ctx context.Context, service *v1.Service, config *v1.Configuration, so *v1.ServiceOrchestrator) (*v1.ServiceOrchestrator, error) {
	//origin := so.DeepCopy()
	so, _ = c.calculateStageRevisionTarget(ctx, so)
	//if equality.Semantic.DeepEqual(origin.Spec, so.Spec) {
	//	return so, nil
	//}
	return c.client.ServingV1().ServiceOrchestrators(service.Namespace).Update(ctx, so, metav1.UpdateOptions{})
}

func (c *Reconciler) reconcileConfiguration(ctx context.Context, service *v1.Service, config *v1.Configuration) (*v1.Configuration, error) {
	existing := config.DeepCopy()
	// In the case of an upgrade, there can be default values set that don't exist pre-upgrade.
	// We are setting the up-to-date default values here so an update won't be triggered if the only
	// diff is the new default values.
	existing.SetDefaults(ctx)

	desiredConfig := resources.MakeConfigurationFromExisting(service, existing)
	equals, err := configSemanticEquals(ctx, desiredConfig, existing)
	if err != nil {
		return nil, err
	}
	if equals {
		return config, nil
	}

	logger := logging.FromContext(ctx)
	logger.Warnf("Service-delegated Configuration %q diff found. Clobbering.", existing.Name)

	// Preserve the rest of the object (e.g. ObjectMeta except for labels).
	existing.Spec = desiredConfig.Spec
	existing.Labels = desiredConfig.Labels
	existing.Annotations = desiredConfig.Annotations
	return c.client.ServingV1().Configurations(service.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
}

func (c *Reconciler) createRoute(ctx context.Context, service *v1.Service) (*v1.Route, error) {
	return c.client.ServingV1().Routes(service.Namespace).Create(
		ctx, resources.MakeRoute(service), metav1.CreateOptions{})
}

func routeSemanticEquals(ctx context.Context, desiredRoute, route *v1.Route) (bool, error) {
	logger := logging.FromContext(ctx)
	specDiff, err := kmp.SafeDiff(desiredRoute.Spec, route.Spec)
	if err != nil {
		logger.Errorw("Error diffing route spec", zap.Error(err))
		return false, fmt.Errorf("failed to diff Route: %w", err)
	} else if specDiff != "" {
		logger.Info("Reconciling route diff (-desired, +observed):\n", specDiff)
	}
	return equality.Semantic.DeepEqual(desiredRoute.Spec, route.Spec) &&
		equality.Semantic.DeepEqual(desiredRoute.Labels, route.Labels) &&
		equality.Semantic.DeepEqual(desiredRoute.Annotations, route.Annotations) &&
		specDiff == "", nil
}

func (c *Reconciler) reconcileRoute(ctx context.Context, service *v1.Service, route *v1.Route) (*v1.Route, error) {
	existing := route.DeepCopy()
	// In the case of an upgrade, there can be default values set that don't exist pre-upgrade.
	// We are setting the up-to-date default values here so an update won't be triggered if the only
	// diff is the new default values.
	existing.SetDefaults(ctx)
	desiredRoute := resources.MakeRoute(service)
	equals, err := routeSemanticEquals(ctx, desiredRoute, existing)
	if err != nil {
		return nil, err
	}
	if equals {
		return route, nil
	}

	// Preserve the rest of the object (e.g. ObjectMeta except for labels and annotations).
	existing.Spec = desiredRoute.Spec
	existing.Labels = desiredRoute.Labels
	existing.Annotations = desiredRoute.Annotations
	return c.client.ServingV1().Routes(service.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
}

// CheckNameAvailability checks that if the named Revision specified by the Configuration
// is available (not found), exists (but matches), or exists with conflict (doesn't match).
//
// TODO(dprotaso) de-dupe once this controller is migrated to v1 apis
func CheckNameAvailability(config *v1.Configuration, lister listers.RevisionLister) error {
	// If config.Spec.GetTemplate().Name is set, then we can directly look up
	// the revision by name.
	name := config.Spec.GetTemplate().Name
	if name == "" {
		return nil
	}
	errConflict := apierrs.NewAlreadyExists(v1.Resource("revisions"), name)

	rev, err := lister.Revisions(config.Namespace).Get(name)
	if err != nil {
		return err
	}

	if !metav1.IsControlledBy(rev, config) {
		// If the revision isn't controller by this configuration, then
		// do not use it.
		return errConflict
	}

	// Check the generation on this revision.
	generationKey := serving.ConfigurationGenerationLabelKey
	expectedValue := configresources.RevisionLabelValueForKey(generationKey, config)
	if rev.Labels != nil && rev.Labels[generationKey] == expectedValue {

		return nil
	}
	// We only require spec equality because the rest is immutable and the user may have
	// annotated or labeled the Revision (beyond what the Configuration might have).
	if !equality.Semantic.DeepEqual(config.Spec.GetTemplate().Spec, rev.Spec) {
		return errConflict
	}
	return nil
}
