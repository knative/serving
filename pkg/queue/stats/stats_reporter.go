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

package stats

import (
	"context"
	"errors"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/metrics"
)

// NOTE: 0 should not be used as boundary. See
// https://github.com/census-ecosystem/opencensus-go-exporter-stackdriver/issues/98
var defaultLatencyDistribution = view.Distribution(5, 10, 20, 40, 60, 80, 100, 150, 200, 250, 300, 350, 400, 450, 500, 600, 700, 800, 900, 1000, 2000, 5000, 10000, 20000, 50000, 100000)

// StatsReporter defines the interface for sending queue proxy metrics.
type StatsReporter interface {
	ReportRequestCount(responseCode int) error
	ReportResponseTime(responseCode int, d time.Duration) error
	ReportQueueDepth(depth int) error
}

// reporter holds cached metric objects to report queue proxy metrics.
type reporter struct {
	ctx             context.Context
	countMetric     *stats.Int64Measure
	latencyMetric   *stats.Float64Measure
	queueSizeMetric *stats.Int64Measure // NB: this can be nil, depending on the reporter.
}

// NewStatsReporter creates a reporter that collects and reports queue proxy metrics.
func NewStatsReporter(ns, service, config, rev, pod string, countMetric *stats.Int64Measure,
	latencyMetric *stats.Float64Measure, queueSizeMetric *stats.Int64Measure) (StatsReporter, error) {
	if ns == "" {
		return nil, errors.New("namespace must not be empty")
	}
	if config == "" {
		return nil, errors.New("config must not be empty")
	}
	if rev == "" {
		return nil, errors.New("revision must not be empty")
	}

	keys := append(metrics.CommonRevisionKeys, metrics.PodTagKey, metrics.ContainerTagKey, metrics.ResponseCodeKey, metrics.ResponseCodeClassKey)
	// Create view to see our measurements.
	if err := view.Register(
		&view.View{
			Description: "The number of requests that are routed to queue-proxy",
			Measure:     countMetric,
			Aggregation: view.Count(),
			TagKeys:     keys,
		},
		&view.View{
			Description: "The response time in millisecond",
			Measure:     latencyMetric,
			Aggregation: defaultLatencyDistribution,
			TagKeys:     keys,
		},
	); err != nil {
		return nil, err
	}
	// If queue size reporter is provided register the view for it too.
	if queueSizeMetric != nil {
		if err := view.Register(
			&view.View{
				Description: "The number of items queued at this queue proxy.",
				Measure:     queueSizeMetric,
				Aggregation: view.LastValue(),
				TagKeys:     keys,
			}); err != nil {
			return nil, err
		}
	}

	// Note that service name can be an empty string, so it needs a special treatment.
	ctx, err := tag.New(
		context.Background(),
		tag.Upsert(metrics.PodTagKey, pod),
		tag.Upsert(metrics.ContainerTagKey, "queue-proxy"),
	)
	if err != nil {
		return nil, err
	}

	ctx, err = metrics.AugmentWithRevision(ctx, ns, service, config, rev)
	if err != nil {
		return nil, err
	}

	return &reporter{
		ctx:             ctx,
		countMetric:     countMetric,
		latencyMetric:   latencyMetric,
		queueSizeMetric: queueSizeMetric,
	}, nil
}

// ReportRequestCount captures request count metric.
func (r *reporter) ReportRequestCount(responseCode int) error {
	pkgmetrics.Record(metrics.AugmentWithResponse(r.ctx, responseCode), r.countMetric.M(1))
	return nil
}

// ReportQueueDepth captures queue depth metric.
func (r *reporter) ReportQueueDepth(d int) error {
	pkgmetrics.Record(r.ctx, r.queueSizeMetric.M(int64(d)))
	return nil
}

// ReportResponseTime captures response time requests
func (r *reporter) ReportResponseTime(responseCode int, d time.Duration) error {
	pkgmetrics.Record(metrics.AugmentWithResponse(r.ctx, responseCode), r.latencyMetric.M(float64(d.Milliseconds())))
	return nil
}
