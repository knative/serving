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

	"github.com/google/go-cmp/cmp"

	caching "github.com/knative/caching/pkg/apis/caching/v1alpha1"
	"github.com/knative/pkg/logging"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
		cfgs.Controller,
	)

	return c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

func (c *Reconciler) createImageCache(ctx context.Context, rev *v1alpha1.Revision, deploy *appsv1.Deployment) (*caching.Image, error) {
	image, err := resources.MakeImageCache(rev, deploy)
	if err != nil {
		return nil, err
	}

	return c.CachingClientSet.CachingV1alpha1().Images(image.Namespace).Create(image)
}

func (c *Reconciler) createKPA(ctx context.Context, rev *v1alpha1.Revision) (*kpa.PodAutoscaler, error) {
	kpa := resources.MakeKPA(rev)

	return c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Create(kpa)
}

type serviceFactory func(*v1alpha1.Revision) *corev1.Service

func (c *Reconciler) createService(ctx context.Context, rev *v1alpha1.Revision, sf serviceFactory) (*corev1.Service, error) {
	// Create the service.
	service := sf(rev)

	return c.KubeClientSet.CoreV1().Services(service.Namespace).Create(service)
}

func (c *Reconciler) checkAndUpdateService(ctx context.Context, rev *v1alpha1.Revision, sf serviceFactory, service *corev1.Service) (*corev1.Service, Changed, error) {
	logger := logging.FromContext(ctx)

	// Note: only reconcile the spec we set.
	rawDesiredService := sf(rev)
	desiredService := service.DeepCopy()
	desiredService.Spec.Selector = rawDesiredService.Spec.Selector
	desiredService.Spec.Ports = rawDesiredService.Spec.Ports

	if equality.Semantic.DeepEqual(desiredService.Spec, service.Spec) {
		return service, Unchanged, nil
	}
	logger.Infof("Reconciling service diff (-desired, +observed): %v",
		cmp.Diff(desiredService.Spec, service.Spec))

	d, err := c.KubeClientSet.CoreV1().Services(service.Namespace).Update(desiredService)
	return d, WasChanged, err
}
