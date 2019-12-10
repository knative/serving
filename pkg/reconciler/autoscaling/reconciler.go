/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package autoscaling

import (
	"context"
	"fmt"
	"reflect"

	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/autoscaling"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	nv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	nlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/resources"
	anames "knative.dev/serving/pkg/reconciler/autoscaling/resources/names"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// Base implements the core controller logic for autoscaling, given a Reconciler.
type Base struct {
	*reconciler.Base
	PALister          listers.PodAutoscalerLister
	ServiceLister     corev1listers.ServiceLister
	SKSLister         nlisters.ServerlessServiceLister
	MetricLister      listers.MetricLister
	ConfigStore       reconciler.ConfigStore
	PSInformerFactory duck.InformerFactory
}

// ReconcileSKS reconciles a ServerlessService based on the given PodAutoscaler.
func (c *Base) ReconcileSKS(ctx context.Context, pa *pav1alpha1.PodAutoscaler, mode nv1alpha1.ServerlessServiceOperationMode) (*nv1alpha1.ServerlessService, error) {
	logger := logging.FromContext(ctx)

	sksName := anames.SKS(pa.Name)
	sks, err := c.SKSLister.ServerlessServices(pa.Namespace).Get(sksName)
	if errors.IsNotFound(err) {
		logger.Info("SKS does not exist; creating.")
		sks = resources.MakeSKS(pa, mode)
		_, err = c.ServingClientSet.NetworkingV1alpha1().ServerlessServices(sks.Namespace).Create(sks)
		if err != nil {
			return nil, fmt.Errorf("error creating SKS %s: %w", sksName, err)
		}
		logger.Info("Created SKS")
	} else if err != nil {
		return nil, fmt.Errorf("error getting SKS %s: %w", sksName, err)
	} else if !metav1.IsControlledBy(sks, pa) {
		pa.Status.MarkResourceNotOwned("ServerlessService", sksName)
		return nil, fmt.Errorf("PA: %s does not own SKS: %s", pa.Name, sksName)
	} else {
		tmpl := resources.MakeSKS(pa, mode)
		if !equality.Semantic.DeepEqual(tmpl.Spec, sks.Spec) {
			want := sks.DeepCopy()
			want.Spec = tmpl.Spec
			logger.Infof("SKS %s changed; reconciling, want mode: %v", sksName, want.Spec.Mode)
			if sks, err = c.ServingClientSet.NetworkingV1alpha1().ServerlessServices(sks.Namespace).Update(want); err != nil {
				return nil, fmt.Errorf("error updating SKS %s: %w", sksName, err)
			}
		}
	}
	logger.Debug("Done reconciling SKS", sksName)
	return sks, nil
}

// DeleteMetricsServices removes all metrics services for the current PA.
// TODO(5900): Remove after 0.12 is cut.
func (c *Base) DeleteMetricsServices(ctx context.Context, pa *pav1alpha1.PodAutoscaler) error {
	logger := logging.FromContext(ctx)

	svcs, err := c.ServiceLister.Services(pa.Namespace).List(labels.SelectorFromSet(map[string]string{
		autoscaling.KPALabelKey:   pa.Name,
		networking.ServiceTypeKey: string(networking.ServiceTypeMetrics),
	}))
	if err != nil {
		return err
	}
	for _, s := range svcs {
		if metav1.IsControlledBy(s, pa) {
			logger.Infof("Removing redundant metric service %s", s.Name)
			if err := c.KubeClientSet.CoreV1().Services(
				s.Namespace).Delete(s.Name, &metav1.DeleteOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReconcileMetric reconciles a metric instance out of the given PodAutoscaler to control metric collection.
func (c *Base) ReconcileMetric(ctx context.Context, pa *pav1alpha1.PodAutoscaler, metricSN string) error {
	desiredMetric := resources.MakeMetric(ctx, pa, metricSN, config.FromContext(ctx).Autoscaler)
	metric, err := c.MetricLister.Metrics(desiredMetric.Namespace).Get(desiredMetric.Name)
	if errors.IsNotFound(err) {
		_, err = c.ServingClientSet.AutoscalingV1alpha1().Metrics(desiredMetric.Namespace).Create(desiredMetric)
		if err != nil {
			return fmt.Errorf("error creating metric: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("error fetching metric: %w", err)
	} else if !metav1.IsControlledBy(metric, pa) {
		pa.Status.MarkResourceNotOwned("Metric", desiredMetric.Name)
		return fmt.Errorf("PA: %s does not own Metric: %s", pa.Name, desiredMetric.Name)
	} else {
		if !equality.Semantic.DeepEqual(desiredMetric.Spec, metric.Spec) {
			want := metric.DeepCopy()
			want.Spec = desiredMetric.Spec
			if _, err = c.ServingClientSet.AutoscalingV1alpha1().Metrics(desiredMetric.Namespace).Update(want); err != nil {
				return fmt.Errorf("error updating metric: %w", err)
			}
		}
	}

	return nil
}

// UpdateStatus updates the status of the given PodAutoscaler.
func (c *Base) UpdateStatus(existing *pav1alpha1.PodAutoscaler, desired *pav1alpha1.PodAutoscaler) error {
	existing = existing.DeepCopy()
	return reconciler.RetryUpdateConflicts(func(attempts int) (err error) {
		// The first iteration tries to use the informer's state, subsequent attempts fetch the latest state via API.
		if attempts > 0 {
			existing, err = c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(desired.Namespace).Get(desired.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		// If there's nothing to update, just return.
		if reflect.DeepEqual(existing.Status, desired.Status) {
			return nil
		}

		existing.Status = desired.Status
		_, err = c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(existing.Namespace).UpdateStatus(existing)
		return err
	})
}
