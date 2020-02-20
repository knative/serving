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
	autoscalingv2beta1listers "k8s.io/client-go/listers/autoscaling/v2beta1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/autoscaling"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	nv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/hpa/resources"
)

// Reconciler implements the control loop for the HPA resources.
type Reconciler struct {
	*areconciler.Base
	hpaLister autoscalingv2beta1listers.HorizontalPodAutoscalerLister
}

// Check that our Reconciler implements pareconciler.Interface
var _ pareconciler.Interface = (*Reconciler)(nil)

func (c *Reconciler) ReconcileKind(ctx context.Context, pa *pav1alpha1.PodAutoscaler) pkgreconciler.Event {
	logger := logging.FromContext(ctx)

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	pa.SetDefaults(ctx)

	pa.Status.InitializeConditions()
	logger.Debug("PA exists")

	// HPA-class PAs don't yet support scale-to-zero
	pa.Status.MarkActive()

	// HPA-class PA delegates autoscaling to the Kubernetes Horizontal Pod Autoscaler.
	desiredHpa := resources.MakeHPA(pa, config.FromContext(ctx).Autoscaler)
	hpa, err := c.hpaLister.HorizontalPodAutoscalers(pa.Namespace).Get(desiredHpa.Name)
	if errors.IsNotFound(err) {
		logger.Infof("Creating HPA %q", desiredHpa.Name)
		if hpa, err = c.KubeClient.AutoscalingV2beta1().HorizontalPodAutoscalers(pa.Namespace).Create(desiredHpa); err != nil {
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
		if _, err := c.KubeClient.AutoscalingV2beta1().HorizontalPodAutoscalers(pa.Namespace).Update(desiredHpa); err != nil {
			return fmt.Errorf("failed to update HPA: %w", err)
		}
	}

	sks, err := c.ReconcileSKS(ctx, pa, nv1alpha1.SKSOperationModeServe)
	if err != nil {
		return fmt.Errorf("error reconciling SKS: %w", err)
	}

	// Only create metrics service and metric entity if we actually need to gather metrics.
	pa.Status.MetricsServiceName = sks.Status.PrivateServiceName
	if pa.Status.MetricsServiceName != "" && pa.Metric() == autoscaling.Concurrency || pa.Metric() == autoscaling.RPS {
		if err := c.ReconcileMetric(ctx, pa, pa.Status.MetricsServiceName); err != nil {
			return fmt.Errorf("error reconciling metric: %w", err)
		}
	}

	// Propagate the service name regardless of the status.
	pa.Status.ServiceName = sks.Status.ServiceName
	if !sks.Status.IsReady() {
		pa.Status.MarkInactive("ServicesNotReady", "SKS Services are not ready yet")
	} else {
		pa.Status.MarkActive()
	}

	// Metrics services are no longer needed as we use the private services now.
	if err := c.DeleteMetricsServices(ctx, pa); err != nil {
		return err
	}

	pa.Status.ObservedGeneration = pa.Generation
	pa.Status.DesiredScale = ptr.Int32(hpa.Status.DesiredReplicas)
	pa.Status.ActualScale = ptr.Int32(hpa.Status.CurrentReplicas)
	return nil
}
