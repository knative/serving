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

package metric

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/types"
	pkgreconciler "knative.dev/pkg/reconciler"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler/metrics"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
)

// reconciler implements controller.Reconciler for Metric resources.
type reconciler struct {
	collector metrics.Collector
	paLister  palisters.PodAutoscalerLister
}

// Check that our Reconciler implements the necessary interfaces.
var (
	_ metricreconciler.Interface        = (*reconciler)(nil)
	_ pkgreconciler.OnDeletionInterface = (*reconciler)(nil)
)

// ReconcileKind implements Interface.ReconcileKind.
func (r *reconciler) ReconcileKind(ctx context.Context, metric *autoscalingv1alpha1.Metric) pkgreconciler.Event {
	// Check if autoscaler in proxy or serving mode, if so pause or resume if needed
	revisionName := metric.Labels[serving.RevisionLabelKey]
	if revisionName != "" {
		paName := revisionName
		pa, err := r.paLister.PodAutoscalers(metric.Namespace).Get(paName)
		if err == nil && pa.Status.MetricsPaused {
			r.collector.Pause(metric)
		} else {
			r.collector.Resume(metric)
		}
	} else {
		// unpause metrics to be safe
		r.collector.Resume(metric)
	}

	if err := r.collector.CreateOrUpdate(metric); err != nil {
		switch {
		case errors.Is(err, metrics.ErrFailedGetEndpoints):
			metric.Status.MarkMetricNotReady("NoEndpoints", err.Error())
		case errors.Is(err, metrics.ErrDidNotReceiveStat):
			metric.Status.MarkMetricFailed("DidNotReceiveStat", err.Error())
		default:
			metric.Status.MarkMetricFailed("CollectionFailed",
				"Failed to reconcile metric collection: "+err.Error())
		}

		// We don't return an error because retrying is of no use. We'll be poked by collector on a change.
		return nil
	}

	metric.Status.MarkMetricReady()
	return nil
}

func (r *reconciler) ObserveDeletion(ctx context.Context, key types.NamespacedName) error {
	r.collector.Delete(key.Namespace, key.Name)
	return nil
}
