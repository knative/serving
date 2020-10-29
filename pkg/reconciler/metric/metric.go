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

	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/metrics"

	pkgreconciler "knative.dev/pkg/reconciler"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"
)

// reconciler implements controller.Reconciler for Metric resources.
type reconciler struct {
	collector metrics.Collector
}

// Check that our Reconciler implements metricreconciler.Interface
var _ metricreconciler.Interface = (*reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (r *reconciler) ReconcileKind(ctx context.Context, metric *v1alpha1.Metric) pkgreconciler.Event {
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
