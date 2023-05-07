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

package hpa

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	autoscalingv2listers "k8s.io/client-go/listers/autoscaling/v2"
	nv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/hpa/resources"
)

// Reconciler implements the control loop for the HPA resources.
type Reconciler struct {
	*areconciler.Base

	kubeClient kubernetes.Interface
	hpaLister  autoscalingv2listers.HorizontalPodAutoscalerLister
}

// Check that our Reconciler implements pareconciler.Interface
var _ pareconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
	defer cancel()

	logger := logging.FromContext(ctx)
	logger.Debug("PA exists")

	// HPA-class PA delegates autoscaling to the Kubernetes Horizontal Pod Autoscaler.
	desiredHpa := resources.MakeHPA(pa, config.FromContext(ctx).Autoscaler)
	hpa, err := c.hpaLister.HorizontalPodAutoscalers(pa.Namespace).Get(desiredHpa.Name)
	if errors.IsNotFound(err) {
		logger.Infof("Creating HPA %q", desiredHpa.Name)
		if hpa, err = c.kubeClient.AutoscalingV2().HorizontalPodAutoscalers(pa.Namespace).Create(ctx, desiredHpa, metav1.CreateOptions{}); err != nil {
			pa.Status.MarkResourceFailedCreation("HorizontalPodAutoscaler", desiredHpa.Name)
			return fmt.Errorf("failed to create HPA: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get HPA: %w", err)
	} else if !metav1.IsControlledBy(hpa, pa) {
		// Surface an error in the PodAutoscaler's status, and return an error.
		pa.Status.MarkResourceNotOwned("HorizontalPodAutoscaler", desiredHpa.Name)
		return fmt.Errorf("PodAutoscaler: %q does not own HPA: %q", pa.Name, desiredHpa.Name)
	}
	if !equality.Semantic.DeepEqual(desiredHpa.Spec, hpa.Spec) {
		logger.Infof("Updating HPA %q", desiredHpa.Name)
		if _, err := c.kubeClient.AutoscalingV2().HorizontalPodAutoscalers(pa.Namespace).Update(ctx, desiredHpa, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update HPA: %w", err)
		}
	}

	// 0 num activators will work as "all".
	sks, err := c.ReconcileSKS(ctx, pa, nv1alpha1.SKSOperationModeServe, 0 /*numActivators*/)
	if err != nil {
		return fmt.Errorf("error reconciling SKS: %w", err)
	}

	// Only create metrics service and metric entity if we actually need to gather metrics.
	pa.Status.MetricsServiceName = sks.Status.PrivateServiceName

	// Propagate the service name regardless of the status.
	pa.Status.ServiceName = sks.Status.ServiceName
	if !sks.IsReady() {
		pa.Status.MarkSKSNotReady("SKS Services are not ready yet")
	} else {
		pa.Status.MarkSKSReady()
		// If a min-scale value has been set, we don't want to mark the scale target
		// as initialized until the current replicas are >= the min-scale value.
		if !pa.Status.IsScaleTargetInitialized() {
			ms := activeThreshold(ctx, pa)
			if hpa.Status.CurrentReplicas >= int32(ms) {
				pa.Status.MarkScaleTargetInitialized()
			}
		}
	}

	// HPA is always _active_.
	pa.Status.MarkActive()

	pa.Status.DesiredScale = ptr.Int32(hpa.Status.DesiredReplicas)
	pa.Status.ActualScale = ptr.Int32(hpa.Status.CurrentReplicas)
	return nil
}

// activeThreshold returns the scale required for the pa to be marked Active
func activeThreshold(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) int {
	asConfig := config.FromContext(ctx).Autoscaler
	min, _ := pa.ScaleBounds(asConfig)
	if !pa.Status.IsScaleTargetInitialized() {
		initialScale := getInitialScale(asConfig, pa)
		return int(intMax(min, initialScale))
	}
	return int(intMax(min, 1))
}

// getInitialScale returns the calculated initial scale based on the autoscaler
// ConfigMap and PA initial scale annotation value.
func getInitialScale(asConfig *autoscalerconfig.Config, pa *autoscalingv1alpha1.PodAutoscaler) int32 {
	initialScale := asConfig.InitialScale
	revisionInitialScale, ok := pa.InitialScale()
	if !ok || (revisionInitialScale == 0 && !asConfig.AllowZeroInitialScale) {
		return initialScale
	}
	return revisionInitialScale
}

func intMax(a, b int32) int32 {
	if a < b {
		return b
	}
	return a
}
