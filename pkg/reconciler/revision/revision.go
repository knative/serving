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
	"strings"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	revisionreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/revision"

	cachinglisters "knative.dev/caching/pkg/client/listers/caching/v1alpha1"
	pkgreconciler "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
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

	resolver resolver
}

// Check that our Reconciler implements revisionreconciler.Interface
var _ revisionreconciler.Interface = (*Reconciler)(nil)

func (c *Reconciler) reconcileDigest(ctx context.Context, rev *v1.Revision) error {
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
		rev.Status.MarkContainerHealthyFalse(v1.ReasonContainerMissing,
			v1.RevisionContainerMissingMessage(
				rev.Spec.GetContainer().Image, err.Error()))
		return err
	}

	rev.Status.ImageDigest = digest

	return nil
}

func (c *Reconciler) ReconcileKind(ctx context.Context, rev *v1.Revision) pkgreconciler.Event {
	readyBeforeReconcile := rev.Status.IsReady()

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	rev.SetDefaults(ctx)
	rev.Status.InitializeConditions()
	c.updateRevisionLoggingURL(ctx, rev)

	phases := []struct {
		name string
		f    func(context.Context, *v1.Revision) error
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

func (c *Reconciler) updateRevisionLoggingURL(ctx context.Context, rev *v1.Revision) {
	config := config.FromContext(ctx)
	if config.Observability.LoggingURLTemplate == "" {
		return
	}

	uid := string(rev.UID)

	rev.Status.LogURL = strings.Replace(
		config.Observability.LoggingURLTemplate,
		"${REVISION_UID}", uid, -1)
}
