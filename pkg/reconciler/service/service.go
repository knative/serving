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
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
	routeLister         listers.RouteLister
}

// Check that our Reconciler implements ksvcreconciler.Interface
var _ ksvcreconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, service *v1.Service) pkgreconciler.Event {
	logger := logging.FromContext(ctx)

	config, err := c.config(ctx, service)
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
		service.Status.MarkRevisionNameTaken(config.Spec.GetTemplate().Name)
		return nil
	}

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
	return nil
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
		service.Status.MarkRouteNotYetReady()
	}
}

func (c *Reconciler) createConfiguration(ctx context.Context, service *v1.Service) (*v1.Configuration, error) {
	cfg, err := resources.MakeConfigurationFromExisting(service, &v1.Configuration{})
	if err != nil {
		return nil, err
	}
	return c.client.ServingV1().Configurations(service.Namespace).Create(ctx, cfg, metav1.CreateOptions{})
}

func configSemanticEquals(ctx context.Context, desiredConfig, config *v1.Configuration) (bool, error) {
	logger := logging.FromContext(ctx)
	specDiff, err := kmp.SafeDiff(desiredConfig.Spec, config.Spec)
	if err != nil {
		logger.Errorw("Error diffing config spec", zap.Error(err))
		return false, fmt.Errorf("failed to diff Configuration: %w", err)
	} else if specDiff != "" {
		logger.Info("Reconciling configuration diff (-desired, +observed):\n", specDiff)
	}
	return equality.Semantic.DeepEqual(desiredConfig.Spec, config.Spec) &&
		equality.Semantic.DeepEqual(desiredConfig.Labels, config.Labels) &&
		equality.Semantic.DeepEqual(desiredConfig.Annotations, config.Annotations) &&
		specDiff == "", nil
}

func (c *Reconciler) reconcileConfiguration(ctx context.Context, service *v1.Service, config *v1.Configuration) (*v1.Configuration, error) {
	existing := config.DeepCopy()
	// In the case of an upgrade, there can be default values set that don't exist pre-upgrade.
	// We are setting the up-to-date default values here so an update won't be triggered if the only
	// diff is the new default values.
	existing.SetDefaults(ctx)
	desiredConfig, err := resources.MakeConfigurationFromExisting(service, existing)
	if err != nil {
		return nil, err
	}

	if equals, err := configSemanticEquals(ctx, desiredConfig, existing); err != nil {
		return nil, err
	} else if equals {
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
	route, err := resources.MakeRoute(service)
	if err != nil {
		// This should be unreachable as configuration creation
		// happens first in `reconcile()` and it verifies the edge cases
		// that would make `MakeRoute` fail as well.
		return nil, err
	}
	return c.client.ServingV1().Routes(service.Namespace).Create(ctx, route, metav1.CreateOptions{})
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
	desiredRoute, err := resources.MakeRoute(service)
	if err != nil {
		// This should be unreachable as configuration creation
		// happens first in `reconcile()` and it verifies the edge cases
		// that would make `MakeRoute` fail as well.
		return nil, err
	}

	if equals, err := routeSemanticEquals(ctx, desiredRoute, existing); err != nil {
		return nil, err
	} else if equals {
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
