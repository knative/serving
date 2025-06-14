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

package revision

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	caching "knative.dev/caching/pkg/apis/caching/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/kmp"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/config"
	"knative.dev/serving/pkg/reconciler/revision/resources"
)

func (c *Reconciler) createDeployment(ctx context.Context, rev *v1.Revision) (*appsv1.Deployment, error) {
	cfgs := config.FromContext(ctx)

	deployment, err := resources.MakeDeployment(rev, cfgs)
	if err != nil {
		return nil, fmt.Errorf("failed to make deployment: %w", err)
	}

	return c.kubeclient.AppsV1().Deployments(deployment.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
}

func (c *Reconciler) checkAndUpdateDeployment(ctx context.Context, rev *v1.Revision, have *appsv1.Deployment) (*appsv1.Deployment, error) {
	logger := logging.FromContext(ctx)
	cfgs := config.FromContext(ctx)

	deployment, err := resources.MakeDeployment(rev, cfgs)
	if err != nil {
		return nil, fmt.Errorf("failed to update deployment: %w", err)
	}

	// We'll update this variable with the result of the update
	var updated *appsv1.Deployment

	err = reconciler.RetryUpdateConflicts(func(attempts int) error {
		// On subsequent attempts, fetch the latest version
		current := have
		if attempts > 0 {
			current, err = c.kubeclient.AppsV1().Deployments(deployment.Namespace).Get(ctx, deployment.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		// Preserve the current scale of the Deployment.
		deployment.Spec.Replicas = current.Spec.Replicas

		// Preserve the label selector since it's immutable.
		// TODO(dprotaso): determine other immutable properties.
		deployment.Spec.Selector = current.Spec.Selector

		// If the spec we want is the spec we have, then we're good.
		if equality.Semantic.DeepEqual(current.Spec, deployment.Spec) {
			updated = current
			return nil
		}

		// Otherwise attempt an update (with ONLY the spec changes).
		desiredDeployment := current.DeepCopy()
		desiredDeployment.Spec = deployment.Spec

		// Carry over new labels.
		desiredDeployment.Labels = kmeta.UnionMaps(deployment.Labels, desiredDeployment.Labels)

		updated, err = c.kubeclient.AppsV1().Deployments(deployment.Namespace).Update(ctx, desiredDeployment, metav1.UpdateOptions{})
		return err
	})

	if err != nil {
		return nil, err
	}

	// If what comes back from the update (with defaults applied by the API server) is the same
	// as what we have then nothing changed.
	if equality.Semantic.DeepEqual(have.Spec, updated.Spec) {
		return updated, nil
	}
	diff, err := kmp.SafeDiff(have.Spec, updated.Spec)
	if err != nil {
		return nil, err
	}

	// If what comes back has a different spec, then signal the change.
	logger.Info("Reconciled deployment diff (-desired, +observed): ", diff)
	return updated, nil
}

func (c *Reconciler) createImageCache(ctx context.Context, rev *v1.Revision, containerName, imageDigest string) (*caching.Image, error) {
	image := resources.MakeImageCache(rev, containerName, imageDigest)
	return c.cachingclient.CachingV1alpha1().Images(image.Namespace).Create(ctx, image, metav1.CreateOptions{})
}

func (c *Reconciler) createPA(
	ctx context.Context,
	rev *v1.Revision,
	deployment *appsv1.Deployment,
) (*autoscalingv1alpha1.PodAutoscaler, error) {
	pa := resources.MakePA(rev, deployment)
	return c.client.AutoscalingV1alpha1().PodAutoscalers(pa.Namespace).Create(ctx, pa, metav1.CreateOptions{})
}
