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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	caching "knative.dev/caching/pkg/apis/caching/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/kmp"
	"knative.dev/pkg/logging"
	autoscaling "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/config"
	"knative.dev/serving/pkg/reconciler/revision/resources"
)

func (c *Reconciler) createDeployment(ctx context.Context, rev *v1.Revision) (*appsv1.Deployment, error) {
	cfgs := config.FromContext(ctx)

	deployment, err := resources.MakeDeployment(
		rev,
		cfgs.Logging,
		cfgs.Tracing,
		cfgs.Network,
		cfgs.Observability,
		cfgs.Autoscaler,
		cfgs.Deployment,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to make deployment: %w", err)
	}

	return c.kubeclient.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

func (c *Reconciler) checkAndUpdateDeployment(ctx context.Context, rev *v1.Revision, have *appsv1.Deployment) (*appsv1.Deployment, error) {
	logger := logging.FromContext(ctx)
	cfgs := config.FromContext(ctx)

	deployment, err := resources.MakeDeployment(
		rev,
		cfgs.Logging,
		cfgs.Tracing,
		cfgs.Network,
		cfgs.Observability,
		cfgs.Autoscaler,
		cfgs.Deployment,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to update deployment: %w", err)
	}

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
	desiredDeployment.Labels = kmeta.UnionMaps(deployment.Labels, desiredDeployment.Labels)

	d, err := c.kubeclient.AppsV1().Deployments(deployment.Namespace).Update(desiredDeployment)
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

func (c *Reconciler) createImageCache(ctx context.Context, rev *v1.Revision) (*caching.Image, error) {
	image := resources.MakeImageCache(rev)

	return c.cachingclient.CachingV1alpha1().Images(image.Namespace).Create(image)
}

func (c *Reconciler) createPA(ctx context.Context, rev *v1.Revision) (*autoscaling.PodAutoscaler, error) {
	pa := resources.MakePA(rev)

	return c.client.AutoscalingV1alpha1().PodAutoscalers(pa.Namespace).Create(pa)
}
