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

package metric

import (
	"context"
	"fmt"

	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/metrics"

	pkgreconciler "knative.dev/pkg/reconciler"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"
	listers "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	rbase "knative.dev/serving/pkg/reconciler"
)

// reconciler implements controller.Reconciler for Metric resources.
type reconciler struct {
	*rbase.Base
	collector    metrics.Collector
	metricLister listers.MetricLister
}

// Check that our Reconciler implements metricreconciler.Interface
var _ metricreconciler.Interface = (*reconciler)(nil)

func (r *reconciler) ReconcileKind(ctx context.Context, metric *v1alpha1.Metric) pkgreconciler.Event {
	metric.SetDefaults(ctx)
	metric.Status.InitializeConditions()

	if err := r.collector.CreateOrUpdate(metric); err != nil {
		// If create or update failes, we won't be able to collect at all.
		metric.Status.MarkMetricFailed("CollectionFailed", "Failed to reconcile metric collection")
		return fmt.Errorf("failed to initiate or update scraping: %w", err)
	}

	metric.Status.MarkMetricReady()
	return nil
}
