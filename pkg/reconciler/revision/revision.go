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

package revision

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	cachinglisters "knative.dev/caching/pkg/client/listers/caching/v1alpha1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/revision/config"
)

type resolver interface {
	Resolve(string, k8schain.Options, sets.String) (string, error)
}

// Reconciler implements controller.Reconciler for Revision resources.
type Reconciler struct {
	*reconciler.Base

	// lister indexes properties about Revision
	revisionLister      listers.RevisionLister
	podAutoscalerLister palisters.PodAutoscalerLister
	imageLister         cachinglisters.ImageLister
	deploymentLister    appsv1listers.DeploymentLister
	serviceLister       corev1listers.ServiceLister
	configMapLister     corev1listers.ConfigMapLister

	resolver    resolver
	configStore reconciler.ConfigStore
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Revision resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)
	ctx = c.configStore.ToContext(ctx)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorw("Invalid resource key", zap.Error(err))
		return nil
	}

	logger.Info("Running reconcile Revision")

	// Get the Revision resource with this namespace/name
	original, err := c.revisionLister.Revisions(namespace).Get(name)
	// The resource may no longer exist, in which case we stop processing.
	if apierrs.IsNotFound(err) {
		logger.Info("Revision in work queue no longer exists")
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informer's copy.
	rev := original.DeepCopy()

	// Reconcile this copy of the revision and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := c.reconcile(ctx, rev)
	if equality.Semantic.DeepEqual(original.Status, rev.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if err = c.updateStatus(original, rev); err != nil {
		logger.Warnw("Failed to update revision status", zap.Error(err))
		c.Recorder.Eventf(rev, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for Revision %q: %v", rev.Name, err)
		return err
	}
	if reconcileErr != nil {
		c.Recorder.Event(rev, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
		return reconcileErr
	}
	// TODO(mattmoor): Remove this after 0.7 cuts.
	// If the spec has changed, then assume we need an upgrade and issue a patch to trigger
	// the webhook to upgrade via defaulting.  Status updates do not trigger this due to the
	// use of the /status resource.
	if !equality.Semantic.DeepEqual(original.Spec, rev.Spec) {
		revisions := v1alpha1.SchemeGroupVersion.WithResource("revisions")
		if err := c.MarkNeedsUpgrade(revisions, rev.Namespace, rev.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Reconciler) reconcileDigest(ctx context.Context, rev *v1alpha1.Revision) error {
	// The image digest has already been resolved.
	if rev.Status.ImageDigest != "" {
		return nil
	}

	var imagePullSecrets []string
	for _, s := range rev.Spec.ImagePullSecrets {
		imagePullSecrets = append(imagePullSecrets, s.Name)
	}
	cfgs := config.FromContext(ctx)
	opt := k8schain.Options{
		Namespace:          rev.Namespace,
		ServiceAccountName: rev.Spec.ServiceAccountName,
		ImagePullSecrets:   imagePullSecrets,
	}
	digest, err := c.resolver.Resolve(rev.Spec.GetContainer().Image,
		opt, cfgs.Deployment.RegistriesSkippingTagResolving)
	if err != nil {
		err = fmt.Errorf("failed to resolve image to digest: %w", err)
		rev.Status.MarkContainerHealthyFalse(v1alpha1.ContainerMissing,
			v1alpha1.RevisionContainerMissingMessage(
				rev.Spec.GetContainer().Image, err.Error()))
		return err
	}

	rev.Status.ImageDigest = digest

	return nil
}

func (c *Reconciler) reconcile(ctx context.Context, rev *v1alpha1.Revision) error {
	if rev.GetDeletionTimestamp() != nil {
		return nil
	}
	readyBeforeReconcile := rev.Status.IsReady()

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	rev.SetDefaults(v1.WithUpgradeViaDefaulting(ctx))

	rev.Status.InitializeConditions()
	c.updateRevisionLoggingURL(ctx, rev)

	if err := rev.ConvertUp(ctx, &v1beta1.Revision{}); err != nil {
		if ce, ok := err.(*v1alpha1.CannotConvertError); ok {
			rev.Status.MarkResourceNotConvertible(ce)
			return nil
		}
		return err
	}

	phases := []struct {
		name string
		f    func(context.Context, *v1alpha1.Revision) error
	}{{
		name: "image digest",
		f:    c.reconcileDigest,
	}, {
		name: "user deployment",
		f:    c.reconcileDeployment,
	}, {
		name: "image cache",
		f:    c.reconcileImageCache,
	}, {
		name: "PA",
		f:    c.reconcilePA,
	}}

	for _, phase := range phases {
		if err := phase.f(ctx, rev); err != nil {
			return err
		}
	}

	readyAfterReconcile := rev.Status.IsReady()
	if !readyBeforeReconcile && readyAfterReconcile {
		c.Recorder.Event(rev, corev1.EventTypeNormal, "RevisionReady",
			"Revision becomes ready upon all resources being ready")
	}

	rev.Status.ObservedGeneration = rev.Generation
	return nil
}

func (c *Reconciler) updateRevisionLoggingURL(
	ctx context.Context,
	rev *v1alpha1.Revision,
) {

	config := config.FromContext(ctx)
	if config.Observability.LoggingURLTemplate == "" {
		return
	}

	uid := string(rev.UID)

	rev.Status.LogURL = strings.Replace(
		config.Observability.LoggingURLTemplate,
		"${REVISION_UID}", uid, -1)
}

func (c *Reconciler) updateStatus(existing *v1alpha1.Revision, desired *v1alpha1.Revision) error {
	existing = existing.DeepCopy()
	return reconciler.RetryUpdateConflicts(func(attempts int) (err error) {
		// The first iteration tries to use the informer's state, subsequent attempts fetch the latest state via API.
		if attempts > 0 {
			existing, err = c.ServingClientSet.ServingV1alpha1().Revisions(desired.Namespace).Get(desired.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		// If there's nothing to update, just return.
		if reflect.DeepEqual(existing.Status, desired.Status) {
			return nil
		}

		existing.Status = desired.Status
		_, err = c.ServingClientSet.ServingV1alpha1().Revisions(desired.Namespace).UpdateStatus(existing)
		return err
	})
}
