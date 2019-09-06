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

	"github.com/pkg/errors"
	"knative.dev/serving/pkg/autoscaler"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	listers "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	rbase "knative.dev/serving/pkg/reconciler"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

const reconcilerName = "Metrics"

// reconciler implements controller.Reconciler for Metric resources.
type reconciler struct {
	*rbase.Base
	collector    autoscaler.Collector
	metricLister listers.MetricLister
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two.
func (r *reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if namespace == "" || err != nil {
		logger.Errorf("Invalid resource key: %s", key)
		return nil
	}

	metric, err := r.metricLister.Metrics(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		return r.collector.Delete(namespace, name)
	} else if err != nil {
		return errors.Wrapf(err, "failed to fetch metric %q", key)
	}

	metric.Status.InitializeConditions()

	if err := r.collector.CreateOrUpdate(metric); err != nil {
		switch {
		case autoscaler.IsErrFailedGetEndpoints(err):
			metric.Status.MarkMetricNotReady("NoEndpoints", "Failed to get endpoints.")
		case autoscaler.IsErrDidNotReceiveStat(err):
			metric.Status.MarkMetricFailed("DidNotReceivedStat", "Did not receive stat from an unscraped pod.")
		default:
			metric.Status.MarkMetricNotReady("CreateOrUpdateFailed", "Collector has failed.")
		}
		return errors.Wrapf(err, "failed to initiate or update scraping")
	}

	metric.Status.MarkMetricReady()
	return nil
}
