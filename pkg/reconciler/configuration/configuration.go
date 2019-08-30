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
	"reflect"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/configuration/resources"
)

// Reconciler implements controller.Reconciler for Configuration resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Configuration
// resource with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name.
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorw("invalid resource key", zap.Error(err))
		return nil
	}
	logger := logging.FromContext(ctx)

	// Get the Configuration resource with this namespace/name.
	original, err := c.configurationLister.Configurations(namespace).Get(name)
	if errors.IsNotFound(err) {
		// The resource no longer exists, in which case we stop processing.
		logger.Error("Configuration in work queue no longer exists")
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informer's copy.
	config := original.DeepCopy()

	// Reconcile this copy of the configuration and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := c.reconcile(ctx, config)
	if equality.Semantic.DeepEqual(original.Status, config.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err = c.updateStatus(config); err != nil {
		logger.Warnw("Failed to update configuration status", zap.Error(err))
		c.Recorder.Eventf(config, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for Configuration %q: %v", config.Name, err)
		return err
	}
	if reconcileErr != nil {
		c.Recorder.Event(config, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
		return reconcileErr
	}
	// TODO(mattmoor): Remove this after 0.7 cuts.
	// If the spec has changed, then assume we need an upgrade and issue a patch to trigger
	// the webhook to upgrade via defaulting.  Status updates do not trigger this due to the
	// use of the /status resource.
	if !equality.Semantic.DeepEqual(original.Spec, config.Spec) {
		configurations := v1alpha1.SchemeGroupVersion.WithResource("configurations")
		if err := c.MarkNeedsUpgrade(configurations, config.Namespace, config.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Reconciler) reconcile(ctx context.Context, config *v1alpha1.Configuration) error {
	logger := logging.FromContext(ctx)
	if config.GetDeletionTimestamp() != nil {
		return nil
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	config.SetDefaults(v1beta1.WithUpgradeViaDefaulting(ctx))
	config.Status.InitializeConditions()

	if err := config.ConvertUp(ctx, &v1beta1.Configuration{}); err != nil {
		if ce, ok := err.(*v1alpha1.CannotConvertError); ok {
			config.Status.MarkResourceNotConvertible(ce)
		}
		return err
	}

	// First, fetch the revision that should exist for the current generation.
	lcr, err := c.latestCreatedRevision(config)
	if errors.IsNotFound(err) {
		lcr, err = c.createRevision(ctx, config)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to create Revision for Configuration %q: %v", config.Name, err)

			logger.Error(errMsg)
			c.Recorder.Event(config, corev1.EventTypeWarning, "CreationFailed", errMsg)

			// Mark the Configuration as not-Ready since creating
			// its latest revision failed.
			config.Status.MarkRevisionCreationFailed(err.Error())

			return err
		}
	} else if errors.IsAlreadyExists(err) {
		// If we get an already-exists error from latestCreatedRevision it means
		// that the Revision name already exists for another Configuration or at
		// the wrong generation of this configuration.
		config.Status.MarkRevisionCreationFailed(err.Error())
		return nil
	} else if err != nil {
		logger.Errorw("Failed to reconcile Configuration: failed to get Revision", zap.Error(err))
		return err
	}

	revName := lcr.Name

	// Second, set this to be the latest revision that we have created.
	config.Status.SetLatestCreatedRevisionName(revName)
	config.Status.ObservedGeneration = config.Generation

	// Last, determine whether we should set LatestReadyRevisionName to our
	// LatestCreatedRevision based on its readiness.
	rc := lcr.Status.GetCondition(v1alpha1.RevisionConditionReady)
	switch {
	case rc == nil || rc.Status == corev1.ConditionUnknown:
		logger.Infof("Revision %q of configuration is not ready", revName)

	case rc.Status == corev1.ConditionTrue:
		logger.Infof("Revision %q of configuration is ready", revName)

		created, ready := config.Status.LatestCreatedRevisionName, config.Status.LatestReadyRevisionName
		if ready == "" {
			// Surface an event for the first revision becoming ready.
			c.Recorder.Event(config, corev1.EventTypeNormal, "ConfigurationReady",
				"Configuration becomes ready")
		}
		// Update the LatestReadyRevisionName and surface an event for the transition.
		config.Status.SetLatestReadyRevisionName(lcr.Name)
		if created != ready {
			c.Recorder.Eventf(config, corev1.EventTypeNormal, "LatestReadyUpdate",
				"LatestReadyRevisionName updated to %q", lcr.Name)
		}

	case rc.Status == corev1.ConditionFalse:
		logger.Infof("Revision %q of configuration has failed", revName)

		// TODO(mattmoor): Only emit the event the first time we see this.
		config.Status.MarkLatestCreatedFailed(lcr.Name, rc.Message)
		c.Recorder.Eventf(config, corev1.EventTypeWarning, "LatestCreatedFailed",
			"Latest created revision %q has failed", lcr.Name)

	default:
		err := fmt.Errorf("unrecognized condition status: %v on revision %q", rc.Status, revName)
		logger.Errorw("Error reconciling Configuration", zap.Error(err))
		return err
	}

	return nil
}

// CheckNameAvailability checks that if the named Revision specified by the Configuration
// is available (not found), exists (but matches), or exists with conflict (doesn't match).
func CheckNameAvailability(config *v1alpha1.Configuration, lister listers.RevisionLister) (*v1alpha1.Revision, error) {
	// If config.Spec.GetTemplate().Name is set, then we can directly look up
	// the revision by name.
	name := config.Spec.GetTemplate().Name
	if name == "" {
		return nil, nil
	}
	errConflict := errors.NewAlreadyExists(v1alpha1.Resource("revisions"), name)

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

func (c *Reconciler) latestCreatedRevision(config *v1alpha1.Configuration) (*v1alpha1.Revision, error) {
	if rev, err := CheckNameAvailability(config, c.revisionLister); rev != nil || err != nil {
		return rev, err
	}

	lister := c.revisionLister.Revisions(config.Namespace)
	generationKey := serving.ConfigurationGenerationLabelKey

	list, err := lister.List(labels.SelectorFromSet(map[string]string{
		generationKey:                 resources.RevisionLabelValueForKey(generationKey, config),
		serving.ConfigurationLabelKey: config.Name,
	}))

	if err == nil && len(list) > 0 {
		return list[0], nil
	}

	return nil, errors.NewNotFound(v1alpha1.Resource("revisions"), fmt.Sprintf("revision for %s", config.Name))
}

func (c *Reconciler) createRevision(ctx context.Context, config *v1alpha1.Configuration) (*v1alpha1.Revision, error) {
	logger := logging.FromContext(ctx)

	rev := resources.MakeRevision(config)
	created, err := c.ServingClientSet.ServingV1alpha1().Revisions(config.Namespace).Create(rev)
	if err != nil {
		return nil, err
	}
	c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Revision %q", created.Name)
	logger.Infof("Created Revision: %#v", created)

	return created, nil
}

func (c *Reconciler) updateStatus(desired *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {
	config, err := c.configurationLister.Configurations(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(config.Status, desired.Status) {
		return config, nil
	}
	// Don't modify the informers copy
	existing := config.DeepCopy()
	existing.Status = desired.Status
	return c.ServingClientSet.ServingV1alpha1().Configurations(desired.Namespace).UpdateStatus(existing)
}
