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
	autoscalingv2beta1listers "k8s.io/client-go/listers/autoscaling/v2beta1"
	nv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/hpa/resources"
)

// Reconciler implements the control loop for the HPA resources.
type Reconciler struct {
	*areconciler.Base

	kubeClient kubernetes.Interface
	hpaLister  autoscalingv2beta1listers.HorizontalPodAutoscalerLister
}

// Check that our Reconciler implements pareconciler.Interface
var _ pareconciler.Interface = (*Reconciler)(nil)

func (c *Reconciler) ReconcileKind(ctx context.Context, pa *pav1alpha1.PodAutoscaler) pkgreconciler.Event {
	logger := logging.FromContext(ctx)
	logger.Debug("PA exists")

	// HPA-class PA delegates autoscaling to the Kubernetes Horizontal Pod Autoscaler.
	desiredHpa := resources.MakeHPA(pa, config.FromContext(ctx).Autoscaler)
	hpa, err := c.hpaLister.HorizontalPodAutoscalers(pa.Namespace).Get(desiredHpa.Name)
	if errors.IsNotFound(err) {
		logger.Infof("Creating HPA %q", desiredHpa.Name)
		if hpa, err = c.kubeClient.AutoscalingV2beta1().HorizontalPodAutoscalers(pa.Namespace).Create(ctx, desiredHpa, metav1.CreateOptions{}); err != nil {
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
		if _, err := c.kubeClient.AutoscalingV2beta1().HorizontalPodAutoscalers(pa.Namespace).Update(ctx, desiredHpa, metav1.UpdateOptions{}); err != nil {
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
		pa.Status.MarkScaleTargetInitialized()
	}
	// HPA is always _active_.
	pa.Status.MarkActive()

	pa.Status.DesiredScale = ptr.Int32(hpa.Status.DesiredReplicas)
	pa.Status.ActualScale = ptr.Int32(hpa.Status.CurrentReplicas)
	return nil
}
