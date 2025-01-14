/*
Copyright 2019 The Knative Authors

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

package autoscaling

import (
	"context"
	"fmt"

	nv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netclientset "knative.dev/networking/pkg/client/clientset/versioned"
	nlisters "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	"knative.dev/pkg/logging"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	listers "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/resources"
	anames "knative.dev/serving/pkg/reconciler/autoscaling/resources/names"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Base implements the core controller logic for autoscaling, given a Reconciler.
type Base struct {
	Client           clientset.Interface
	NetworkingClient netclientset.Interface
	SKSLister        nlisters.ServerlessServiceLister
	MetricLister     listers.MetricLister
}

// ReconcileSKS reconciles a ServerlessService based on the given PodAutoscaler.
func (c *Base) ReconcileSKS(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler,
	mode nv1alpha1.ServerlessServiceOperationMode, numActivators int32,
) (*nv1alpha1.ServerlessService, error) {
	logger := logging.FromContext(ctx)

	sksName := anames.SKS(pa.Name)
	sks, err := c.SKSLister.ServerlessServices(pa.Namespace).Get(sksName)
	if errors.IsNotFound(err) {
		logger.Info("SKS does not exist; creating.")
		sks = resources.MakeSKS(pa, mode, numActivators)
		if _, err = c.NetworkingClient.NetworkingV1alpha1().ServerlessServices(sks.Namespace).Create(ctx, sks, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("error creating SKS %s: %w", sksName, err)
		}
		logger.Info("Created SKS")
	} else if err != nil {
		return nil, fmt.Errorf("error getting SKS %s: %w", sksName, err)
	} else if !metav1.IsControlledBy(sks, pa) {
		pa.Status.MarkResourceNotOwned("ServerlessService", sksName)
		return nil, fmt.Errorf("PA: %s does not own SKS: %s", pa.Name, sksName)
	} else {
		tmpl := resources.MakeSKS(pa, mode, numActivators)
		if !equality.Semantic.DeepEqual(tmpl.Spec, sks.Spec) {
			want := sks.DeepCopy()
			want.Spec = tmpl.Spec
			logger.Infof("SKS %s changed; reconciling, want mode: %v", sksName, want.Spec.Mode)
			if sks, err = c.NetworkingClient.NetworkingV1alpha1().ServerlessServices(sks.Namespace).Update(ctx, want, metav1.UpdateOptions{}); err != nil {
				return nil, fmt.Errorf("error updating SKS %s: %w", sksName, err)
			}
		}
	}
	logger.Debug("Done reconciling SKS ", sksName)
	return sks, nil
}

// ReconcileMetric reconciles a metric instance out of the given PodAutoscaler to control metric collection.
func (c *Base) ReconcileMetric(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler, metricSN string) error {
	desiredMetric := resources.MakeMetric(pa, metricSN, config.FromContext(ctx).Autoscaler)
	metric, err := c.MetricLister.Metrics(desiredMetric.Namespace).Get(desiredMetric.Name)
	if errors.IsNotFound(err) {
		_, err = c.Client.AutoscalingV1alpha1().Metrics(desiredMetric.Namespace).Create(ctx, desiredMetric, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("error creating metric: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("error fetching metric: %w", err)
	} else if !metav1.IsControlledBy(metric, pa) {
		pa.Status.MarkResourceNotOwned("Metric", desiredMetric.Name)
		return fmt.Errorf("PA: %s does not own Metric: %s", pa.Name, desiredMetric.Name)
	} else if !equality.Semantic.DeepEqual(desiredMetric.Spec, metric.Spec) {
		want := metric.DeepCopy()
		want.Spec = desiredMetric.Spec
		if _, err = c.Client.AutoscalingV1alpha1().Metrics(desiredMetric.Namespace).Update(ctx, want, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("error updating metric: %w", err)
		}
	}

	return nil
}
