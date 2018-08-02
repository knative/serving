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
	"github.com/google/go-cmp/cmp/cmpopts"

	commonlogging "github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/reconciler/revision/config"
	"github.com/knative/serving/pkg/reconciler/revision/resources"

	"go.uber.org/zap"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	vpav1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"
)

func (c *Reconciler) createDeployment(ctx context.Context, rev *v1alpha1.Revision) (*appsv1.Deployment, error) {
	logger := logging.FromContext(ctx)

	var replicaCount int32 = 1
	if rev.Spec.ServingState == v1alpha1.RevisionServingStateReserve {
		replicaCount = 0
	}
	deployment := resources.MakeDeployment(rev, c.getLoggingConfig(), c.getNetworkConfig(),
		c.getObservabilityConfig(), c.getAutoscalerConfig(), c.getControllerConfig(), replicaCount)

	// Resolve tag image references to digests.
	if err := c.getResolver().Resolve(deployment); err != nil {
		logger.Error("Error resolving deployment", zap.Error(err))
		rev.Status.MarkContainerMissing(err.Error())
		return nil, fmt.Errorf("Error resolving container to digest: %v", err)
	}

	return c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

// This is a generic function used both for deployment of user code & autoscaler
func (c *Reconciler) checkAndUpdateDeployment(ctx context.Context, rev *v1alpha1.Revision, deployment *appsv1.Deployment) (*appsv1.Deployment, Changed, error) {
	logger := logging.FromContext(ctx)

	// TODO(mattmoor): Generalize this to reconcile discrepancies vs. what
	// resources.MakeDeployment() would produce.
	desiredDeployment := deployment.DeepCopy()
	if desiredDeployment.Spec.Replicas == nil {
		var one int32 = 1
		desiredDeployment.Spec.Replicas = &one
	}
	if rev.Spec.ServingState == v1alpha1.RevisionServingStateActive && *desiredDeployment.Spec.Replicas == 0 {
		*desiredDeployment.Spec.Replicas = 1
	} else if rev.Spec.ServingState == v1alpha1.RevisionServingStateReserve && *desiredDeployment.Spec.Replicas != 0 {
		*desiredDeployment.Spec.Replicas = 0
	}

	if equality.Semantic.DeepEqual(desiredDeployment.Spec, deployment.Spec) {
		return deployment, Unchanged, nil
	}
	logger.Infof("Reconciling deployment diff (-desired, +observed): %v",
		cmp.Diff(desiredDeployment.Spec, deployment.Spec, cmpopts.IgnoreUnexported(resource.Quantity{})))
	deployment.Spec = desiredDeployment.Spec
	d, err := c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Update(deployment)
	return d, WasChanged, err
}

// This is a generic function used both for deployment of user code & autoscaler
func (c *Reconciler) deleteDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	logger := logging.FromContext(ctx)

	err := c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Delete(deployment.Name, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("deployments.Delete for %q failed: %s", deployment.Name, err)
		return err
	}
	return nil
}

type serviceFactory func(*v1alpha1.Revision) *corev1.Service

func (c *Reconciler) createService(ctx context.Context, rev *v1alpha1.Revision, sf serviceFactory) (*corev1.Service, error) {
	// Create the service.
	service := sf(rev)

	return c.KubeClientSet.CoreV1().Services(service.Namespace).Create(service)
}

func (c *Reconciler) checkAndUpdateService(ctx context.Context, rev *v1alpha1.Revision, sf serviceFactory, service *corev1.Service) (*corev1.Service, Changed, error) {
	logger := logging.FromContext(ctx)

	desiredService := sf(rev)

	// Preserve the ClusterIP field in the Service's Spec, if it has been set.
	desiredService.Spec.ClusterIP = service.Spec.ClusterIP

	if equality.Semantic.DeepEqual(desiredService.Spec, service.Spec) {
		return service, Unchanged, nil
	}
	logger.Infof("Reconciling service diff (-desired, +observed): %v",
		cmp.Diff(desiredService.Spec, service.Spec))
	service.Spec = desiredService.Spec

	d, err := c.KubeClientSet.CoreV1().Services(service.Namespace).Update(service)
	return d, WasChanged, err
}

func (c *Reconciler) deleteService(ctx context.Context, svc *corev1.Service) error {
	logger := logging.FromContext(ctx)

	err := c.KubeClientSet.CoreV1().Services(svc.Namespace).Delete(svc.Name, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("service.Delete for %q failed: %s", svc.Name, err)
		return err
	}
	return nil
}

func (c *Reconciler) createAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) (*appsv1.Deployment, error) {
	var replicaCount int32 = 1
	if rev.Spec.ServingState == v1alpha1.RevisionServingStateReserve {
		replicaCount = 0
	}
	deployment := resources.MakeAutoscalerDeployment(rev, c.getControllerConfig().AutoscalerImage, replicaCount)
	return c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

func (c *Reconciler) createVPA(ctx context.Context, rev *v1alpha1.Revision) (*vpav1alpha1.VerticalPodAutoscaler, error) {
	vpa := resources.MakeVPA(rev)

	return c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(vpa.Namespace).Create(vpa)
}

func (c *Reconciler) deleteVPA(ctx context.Context, vpa *vpav1alpha1.VerticalPodAutoscaler) error {
	logger := logging.FromContext(ctx)
	if !c.getAutoscalerConfig().EnableVPA {
		return nil
	}

	err := c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(vpa.Namespace).Delete(vpa.Name, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("vpa.Delete for %q failed: %v", vpa.Name, err)
		return err
	}
	return nil
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
