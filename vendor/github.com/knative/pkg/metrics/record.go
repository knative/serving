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

package metrics

import (
	"context"
	"path"

	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/metrics/metricskey"
	"go.opencensus.io/stats"
)

// Record decides whether to record one measurement based on current
// metrics config. If yes, it records measurements via OpenCensus.
func Record(ctx context.Context, ms stats.Measurement) {
	mc := getCurMetricsConfig()
	if mc == nil {
		logging.FromContext(ctx).Warn("The current metrics config has not been successfully set yet, not record the metric %v.", ms)
		return
	}
	// Should record if one of the following conditions satisfies:
	// 1) the backend is not Stackdriver
	// 2) the backend is Stackdriver and it is allowed to use custom metrics
	// 3) the backend is Stackdriver and the metric is "knative_revison" built-in metric.
	if !mc.isStackdriverBackend || mc.allowStackdriverCustomMetrics {
		stats.Record(ctx, ms)
	} else {
		metricType := path.Join(mc.stackdriverMetricTypePrefix, ms.Measure().Name())
		if metricskey.KnativeRevisionMetrics.Has(metricType) {
			stats.Record(ctx, ms)
		}
	}
}
