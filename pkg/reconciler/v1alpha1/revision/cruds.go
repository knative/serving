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

	"github.com/google/go-cmp/cmp"

	"github.com/knative/pkg/logging"
	commonlogging "github.com/knative/pkg/logging"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	vpav1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"

	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
)

func (c *Reconciler) createDeployment(ctx context.Context, rev *v1alpha1.Revision) (*appsv1.Deployment, error) {
	logger := logging.FromContext(ctx)

	deployment := resources.MakeDeployment(rev, c.getLoggingConfig(), c.getNetworkConfig(),
		c.getObservabilityConfig(), c.getAutoscalerConfig(), c.getControllerConfig())

	// Resolve tag image references to digests.
	if err := c.getResolver().Resolve(deployment); err != nil {
		logger.Error("Error resolving deployment", zap.Error(err))
		rev.Status.MarkContainerMissing(err.Error())
		return nil, fmt.Errorf("Error resolving container to digest: %v", err)
	}

	return c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

func (c *Reconciler) createKPA(ctx context.Context, rev *v1alpha1.Revision) (*kpa.PodAutoscaler, error) {
	kpa := resources.MakeKPA(rev)

	return c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Create(kpa)
}

func (c *Reconciler) checkAndUpdateKPA(ctx context.Context, rev *v1alpha1.Revision, kpa *kpa.PodAutoscaler) (*kpa.PodAutoscaler, Changed, error) {
	logger := logging.FromContext(ctx)

	desiredKPA := resources.MakeKPA(rev)
	// TODO(mattmoor): Preserve the serving state on the KPA (once it is the source of truth)
	// desiredKPA.Spec.ServingState = kpa.Spec.ServingState
	desiredKPA.Spec.Generation = kpa.Spec.Generation
	if equality.Semantic.DeepEqual(desiredKPA.Spec, kpa.Spec) && equality.Semantic.DeepEqual(desiredKPA.Annotations, kpa.Annotations) {
		return kpa, Unchanged, nil
	}
	logger.Infof("Reconciling kpa diff (-desired, +observed): %v", cmp.Diff(desiredKPA.Spec, kpa.Spec))
	kpa.Spec = desiredKPA.Spec
	kpa.Annotations = desiredKPA.Annotations
	kpa, err := c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Update(kpa)
	return kpa, WasChanged, err
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
	service.Spec = desiredService.Spec

	d, err := c.KubeClientSet.CoreV1().Services(service.Namespace).Update(service)
	return d, WasChanged, err
}

func (c *Reconciler) createVPA(ctx context.Context, rev *v1alpha1.Revision) (*vpav1alpha1.VerticalPodAutoscaler, error) {
	vpa := resources.MakeVPA(rev)

	return c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(vpa.Namespace).Create(vpa)
}

func (c *Reconciler) getNetworkConfig() *config.Network {
	c.networkConfigMutex.Lock()
	defer c.networkConfigMutex.Unlock()
	return c.networkConfig.DeepCopy()
}

func (c *Reconciler) getResolver() resolver {
	c.resolverMutex.Lock()
	defer c.resolverMutex.Unlock()
	return c.resolver
}

func (c *Reconciler) getControllerConfig() *config.Controller {
	c.controllerConfigMutex.Lock()
	defer c.controllerConfigMutex.Unlock()
	return c.controllerConfig.DeepCopy()
}

func (c *Reconciler) getLoggingConfig() *commonlogging.Config {
	c.loggingConfigMutex.Lock()
	defer c.loggingConfigMutex.Unlock()
	return c.loggingConfig.DeepCopy()
}

func (c *Reconciler) getObservabilityConfig() *config.Observability {
	c.observabilityConfigMutex.Lock()
	defer c.observabilityConfigMutex.Unlock()
	return c.observabilityConfig.DeepCopy()
}

func (c *Reconciler) getAutoscalerConfig() *autoscaler.Config {
	c.autoscalerConfigMutex.Lock()
	defer c.autoscalerConfigMutex.Unlock()
	return c.autoscalerConfig.DeepCopy()
}
