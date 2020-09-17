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
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	cachingclientset "knative.dev/caching/pkg/client/clientset/versioned"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	revisionreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/revision"

	cachinglisters "knative.dev/caching/pkg/client/listers/caching/v1alpha1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/reconciler/revision/config"
)

const digestResolutionTimeout = 60 * time.Second

type resolver interface {
	Resolve(*v1.Revision, k8schain.Options, sets.String, time.Duration) ([]v1.ContainerStatus, error)
	Clear(types.NamespacedName)
}

// Reconciler implements controller.Reconciler for Revision resources.
type Reconciler struct {
	kubeclient    kubernetes.Interface
	client        clientset.Interface
	cachingclient cachingclientset.Interface

	// lister indexes properties about Revision
	podAutoscalerLister palisters.PodAutoscalerLister
	imageLister         cachinglisters.ImageLister
	deploymentLister    appsv1listers.DeploymentLister

	resolver resolver
}

// Check that our Reconciler implements revisionreconciler.Interface
var _ revisionreconciler.Interface = (*Reconciler)(nil)

func (c *Reconciler) reconcileDigest(ctx context.Context, rev *v1.Revision) (bool, error) {
	if rev.Status.ContainerStatuses == nil {
		rev.Status.ContainerStatuses = make([]v1.ContainerStatus, 0, len(rev.Spec.Containers))
	}

	if rev.Status.DeprecatedImageDigest != "" {
		// Default old revisions to have ContainerStatuses filled in.
		// This path should only be taken by "old" revisions that have exactly one container.
		if len(rev.Status.ContainerStatuses) == 0 {
			rev.Status.ContainerStatuses = append(rev.Status.ContainerStatuses, v1.ContainerStatus{
				Name:        rev.Spec.Containers[0].Name,
				ImageDigest: rev.Status.DeprecatedImageDigest,
			})
		}
	}

	// The image digest has already been resolved.
	if len(rev.Status.ContainerStatuses) == len(rev.Spec.Containers) {
		c.resolver.Clear(types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name})
		return true, nil
	}

	imagePullSecrets := make([]string, 0, len(rev.Spec.ImagePullSecrets))
	for _, s := range rev.Spec.ImagePullSecrets {
		imagePullSecrets = append(imagePullSecrets, s.Name)
	}
	cfgs := config.FromContext(ctx)
	opt := k8schain.Options{
		Namespace:          rev.Namespace,
		ServiceAccountName: rev.Spec.ServiceAccountName,
		ImagePullSecrets:   imagePullSecrets,
	}

	statuses, err := c.resolver.Resolve(rev, opt, cfgs.Deployment.RegistriesSkippingTagResolving, digestResolutionTimeout)
	if err != nil {
		rev.Status.MarkContainerHealthyFalse(v1.ReasonContainerMissing, err.Error())
		return true, err
	}
	if len(statuses) > 0 {
		rev.Status.ContainerStatuses = statuses

		// For backwards-compatibility we need to continue to set the DeprecatedImageDigest field.
		for i := range rev.Spec.Containers {
			if len(rev.Spec.Containers) == 1 || len(rev.Spec.Containers[i].Ports) != 0 {
				rev.Status.DeprecatedImageDigest = statuses[i].ImageDigest
			}
		}

		return true, nil
	}

	// No digest yet, wait for re-enqueue when resolution is done.
	return false, nil
}

func (c *Reconciler) ReconcileKind(ctx context.Context, rev *v1.Revision) pkgreconciler.Event {
	readyBeforeReconcile := rev.IsReady()
	c.updateRevisionLoggingURL(ctx, rev)

	reconciled, err := c.reconcileDigest(ctx, rev)
	if err != nil {
		return err
	}
	if !reconciled {
		// Digest not resolved yet, wait for background resolution to re-enqueue the revision.
		rev.Status.MarkResourcesAvailableUnknown(v1.ReasonResolvingDigests, "")
		return nil
	}

	for _, phase := range []func(context.Context, *v1.Revision) error{
		c.reconcileDeployment,
		c.reconcileImageCache,
		c.reconcilePA,
	} {
		if err := phase(ctx, rev); err != nil {
			return err
		}
	}
	readyAfterReconcile := rev.Status.GetCondition(v1.RevisionConditionReady).IsTrue()
	if !readyBeforeReconcile && readyAfterReconcile {
		logging.FromContext(ctx).Info("Revision became ready")
		controller.GetEventRecorder(ctx).Event(
			rev, corev1.EventTypeNormal, "RevisionReady",
			"Revision becomes ready upon all resources being ready")
	}

	return nil
}

func (c *Reconciler) updateRevisionLoggingURL(ctx context.Context, rev *v1.Revision) {
	config := config.FromContext(ctx)
	if config.Observability.LoggingURLTemplate == "" {
		return
	}

	rev.Status.LogURL = strings.ReplaceAll(
		config.Observability.LoggingURLTemplate,
		"${REVISION_UID}", string(rev.UID))
}
