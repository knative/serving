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

	caching "github.com/knative/caching/pkg/apis/caching/v1alpha1"
	"github.com/knative/pkg/kmp"
	"github.com/knative/pkg/logging"
	kpav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/revision/config"
	"github.com/knative/serving/pkg/reconciler/revision/resources"
	presources "github.com/knative/serving/pkg/resources"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

func (c *Reconciler) createDeployment(ctx context.Context, rev *v1alpha1.Revision) (*appsv1.Deployment, error) {
	cfgs := config.FromContext(ctx)

	deployment := resources.MakeDeployment(
		rev,
		cfgs.Logging,
		cfgs.Network,
		cfgs.Observability,
		cfgs.Autoscaler,
		cfgs.Deployment,
	)

	return c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

func (c *Reconciler) checkAndUpdateDeployment(ctx context.Context, rev *v1alpha1.Revision, have *appsv1.Deployment) (*appsv1.Deployment, error) {
	logger := logging.FromContext(ctx)
	cfgs := config.FromContext(ctx)

	deployment := resources.MakeDeployment(
		rev,
		cfgs.Logging,
		cfgs.Network,
		cfgs.Observability,
		cfgs.Autoscaler,
		cfgs.Deployment,
	)

	// Preserve the current scale of the Deployment.
	deployment.Spec.Replicas = have.Spec.Replicas

	// Preserve the label selector since it's immutable.
	// TODO(dprotaso): determine other immutable properties.
	deployment.Spec.Selector = have.Spec.Selector

	// If the spec we want is the spec we have, then we're good.
	if equality.Semantic.DeepEqual(have.Spec, deployment.Spec) {
		return have, nil
	}

	// Otherwise attempt an update (with ONLY the spec changes).
	desiredDeployment := have.DeepCopy()
	desiredDeployment.Spec = deployment.Spec

	// Carry over new labels.
	desiredDeployment.Labels = presources.UnionMaps(deployment.Labels, desiredDeployment.Labels)

	d, err := c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Update(desiredDeployment)
	if err != nil {
		return nil, err
	}

	// If what comes back from the update (with defaults applied by the API server) is the same
	// as what we have then nothing changed.
	if equality.Semantic.DeepEqual(have.Spec, d.Spec) {
		return d, nil
	}
	diff, err := kmp.SafeDiff(have.Spec, d.Spec)
	if err != nil {
		return nil, err
	}

	// If what comes back has a different spec, then signal the change.
	logger.Infof("Reconciled deployment diff (-desired, +observed): %v", diff)
	return d, nil
}

func (c *Reconciler) createImageCache(ctx context.Context, rev *v1alpha1.Revision) (*caching.Image, error) {
	image := resources.MakeImageCache(rev)

	return c.CachingClientSet.CachingV1alpha1().Images(image.Namespace).Create(image)
}

func (c *Reconciler) createKPA(ctx context.Context, rev *v1alpha1.Revision) (*kpav1alpha1.PodAutoscaler, error) {
	kpa := resources.MakeKPA(rev)

	return c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Create(kpa)
}
