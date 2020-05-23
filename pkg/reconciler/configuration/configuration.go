/*
Copyright 2018 The Knative Authors.

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
	"context"
	"fmt"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/reconciler/configuration/resources"
)

// Reconciler implements controller.Reconciler for Configuration resources.
type Reconciler struct {
	client clientset.Interface

	// listers index properties about resources
	revisionLister listers.RevisionLister
}

// Check that our Reconciler implements configreconciler.Interface
var _ configreconciler.Interface = (*Reconciler)(nil)

func (c *Reconciler) ReconcileKind(ctx context.Context, config *v1.Configuration) pkgreconciler.Event {
	logger := logging.FromContext(ctx)
	recorder := controller.GetEventRecorder(ctx)

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	config.SetDefaults(ctx)
	config.Status.InitializeConditions()

	// Bump observed generation to denote that we have processed this
	// generation regardless of success or failure.
	config.Status.ObservedGeneration = config.Generation

	// First, fetch the revision that should exist for the current generation.
	lcr, err := c.latestCreatedRevision(config)
	if errors.IsNotFound(err) {
		lcr, err = c.createRevision(ctx, config)
		if err != nil {
			recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision: %v", err)

			// Mark the Configuration as not-Ready since creating
			// its latest revision failed.
			config.Status.MarkRevisionCreationFailed(err.Error())

			return fmt.Errorf("failed to create Revision: %w", err)
		}
	} else if errors.IsAlreadyExists(err) {
		// If we get an already-exists error from latestCreatedRevision it means
		// that the Revision name already exists for another Configuration or at
		// the wrong generation of this configuration.
		config.Status.MarkRevisionCreationFailed(err.Error())
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get Revision: %w", err)
	}

	revName := lcr.Name

	// Second, set this to be the latest revision that we have created.
	config.Status.SetLatestCreatedRevisionName(revName)

	// Last, determine whether we should set LatestReadyRevisionName to our
	// LatestCreatedRevision based on its readiness.
	rc := lcr.Status.GetCondition(v1.RevisionConditionReady)
	switch {
	case rc == nil || rc.Status == corev1.ConditionUnknown:
		logger.Infof("Revision %q of configuration is not ready", revName)

	case rc.Status == corev1.ConditionTrue:
		logger.Infof("Revision %q of configuration is ready", revName)
		if config.Status.LatestReadyRevisionName == "" {
			// Surface an event for the first revision becoming ready.
			recorder.Event(config, corev1.EventTypeNormal, "ConfigurationReady",
				"Configuration becomes ready")
		}

	case rc.Status == corev1.ConditionFalse:
		logger.Infof("Revision %q of configuration has failed", revName)
		beforeReady := config.Status.GetCondition(v1.ConfigurationConditionReady)
		config.Status.MarkLatestCreatedFailed(lcr.Name, rc.Message)

		if !equality.Semantic.DeepEqual(beforeReady, config.Status.GetCondition(v1.ConfigurationConditionReady)) {
			recorder.Eventf(config, corev1.EventTypeWarning, "LatestCreatedFailed",
				"Latest created revision %q has failed", lcr.Name)
		}

	default:
		return fmt.Errorf("unrecognized condition status: %v on revision %q", rc.Status, revName)
	}

	if err = c.findAndSetLatestReadyRevision(ctx, config); err != nil {
		return fmt.Errorf("failed to find and set latest ready revision: %w", err)
	}
	return nil
}

// findAndSetLatestReadyRevision finds the last ready revision and sets LatestReadyRevisionName to it.
func (c *Reconciler) findAndSetLatestReadyRevision(ctx context.Context, config *v1.Configuration) error {
	sortedRevisions, err := c.getSortedCreatedRevisions(ctx, config)
	if err != nil {
		return err
	}
	for _, rev := range sortedRevisions {
		if rev.Status.IsReady() {
			old, new := config.Status.LatestReadyRevisionName, rev.Name
			config.Status.SetLatestReadyRevisionName(rev.Name)
			if old != new {
				controller.GetEventRecorder(ctx).Eventf(
					config, corev1.EventTypeNormal, "LatestReadyUpdate",
					"LatestReadyRevisionName updated to %q", rev.Name)
			}
			return nil
		}
	}
	return nil
}

// getSortedCreatedRevisions returns the list of created revisions sorted in descending
// generation order between the generation of the latest ready revision and config's generation (both inclusive).
func (c *Reconciler) getSortedCreatedRevisions(ctx context.Context, config *v1.Configuration) ([]*v1.Revision, error) {
	logger := logging.FromContext(ctx)
	lister := c.revisionLister.Revisions(config.Namespace)
	configSelector := labels.SelectorFromSet(labels.Set{
		serving.ConfigurationLabelKey: config.Name,
	})
	if config.Status.LatestReadyRevisionName != "" {
		lrr, err := lister.Get(config.Status.LatestReadyRevisionName)
		// Record the error and continue because we still want to set the LRR to the correct revision.
		if err != nil {
			// If the user deletes the LatestReadyRevision then this may return an error due to the
			// dangling reference.  Proceed to calculate the next-latest ready revision so that the
			// caller can synthesize a new Revision at the current generation to replace the one deleted.
			logger.Errorf("Error getting latest ready revision %q: %v", config.Status.LatestReadyRevisionName, err)
		} else {
			start := lrr.Generation
			var generations []string
			for i := start; i <= config.Generation; i++ {
				generations = append(generations, strconv.FormatInt(i, 10))
			}

			// Add an "In" filter so that the configurations we get back from List have generation
			// in range (config's latest ready generation, config's generation]
			generationKey := serving.ConfigurationGenerationLabelKey
			inReq, err := labels.NewRequirement(generationKey,
				selection.In,
				generations,
			)
			if err == nil {
				configSelector = configSelector.Add(*inReq)
			}
		}
	}

	list, err := lister.List(configSelector)
	if err != nil {
		return nil, err
	}
	// Return a sorted list with Generation in descending order
	if len(list) > 1 {
		sort.Slice(list, func(i, j int) bool {
			// BYO name always be the first
			if config.Spec.Template.Name == list[i].Name {
				return true
			}
			if config.Spec.Template.Name == list[j].Name {
				return false
			}
			intI, errI := strconv.Atoi(list[i].Labels[serving.ConfigurationGenerationLabelKey])
			intJ, errJ := strconv.Atoi(list[j].Labels[serving.ConfigurationGenerationLabelKey])
			if errI != nil || errJ != nil {
				return true
			}
			return intI > intJ
		})
	}
	return list, nil
}

// CheckNameAvailability checks that if the named Revision specified by the Configuration
// is available (not found), exists (but matches), or exists with conflict (doesn't match).
func CheckNameAvailability(config *v1.Configuration, lister listers.RevisionLister) (*v1.Revision, error) {
	// If config.Spec.GetTemplate().Name is set, then we can directly look up
	// the revision by name.
	name := config.Spec.GetTemplate().Name
	if name == "" {
		return nil, nil
	}
	errConflict := errors.NewAlreadyExists(v1.Resource("revisions"), name)

	rev, err := lister.Revisions(config.Namespace).Get(name)
	if errors.IsNotFound(err) {
		// Does not exist, we must be good!
		// note: for the name to change the generation must change.
		return nil, err
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(rev, config) {
		// If the revision isn't controller by this configuration, then
		// do not use it.
		return nil, errConflict
	}

	// Check the generation on this revision.
	generationKey := serving.ConfigurationGenerationLabelKey
	expectedValue := resources.RevisionLabelValueForKey(generationKey, config)
	if rev.Labels != nil && rev.Labels[generationKey] == expectedValue {
		return rev, nil
	}
	// We only require spec equality because the rest is immutable and the user may have
	// annotated or labeled the Revision (beyond what the Configuration might have).
	if !equality.Semantic.DeepEqual(config.Spec.GetTemplate().Spec, rev.Spec) {
		return nil, errConflict
	}
	return rev, nil
}

func (c *Reconciler) latestCreatedRevision(config *v1.Configuration) (*v1.Revision, error) {
	if rev, err := CheckNameAvailability(config, c.revisionLister); rev != nil || err != nil {
		return rev, err
	}

	lister := c.revisionLister.Revisions(config.Namespace)
	generationKey := serving.ConfigurationGenerationLabelKey

	list, err := lister.List(labels.SelectorFromSet(labels.Set{
		generationKey:                 resources.RevisionLabelValueForKey(generationKey, config),
		serving.ConfigurationLabelKey: config.Name,
	}))

	if err == nil && len(list) > 0 {
		return list[0], nil
	}

	return nil, errors.NewNotFound(v1.Resource("revisions"), fmt.Sprintf("revision for %s", config.Name))
}

func (c *Reconciler) createRevision(ctx context.Context, config *v1.Configuration) (*v1.Revision, error) {
	logger := logging.FromContext(ctx)

	rev := resources.MakeRevision(config)
	created, err := c.client.ServingV1().Revisions(config.Namespace).Create(rev)
	if err != nil {
		return nil, err
	}
	controller.GetEventRecorder(ctx).Eventf(config, corev1.EventTypeNormal, "Created", "Created Revision %q", created.Name)
	logger.Infof("Created Revision: %#v", created)

	return created, nil
}
