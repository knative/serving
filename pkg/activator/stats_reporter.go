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

package activator

import (
	"context"
	"strconv"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/serving/pkg/metrics"
)

var (
	requestConcurrencyM = stats.Int64(
		"request_concurrency",
		"Concurrent requests that are routed to Activator",
		stats.UnitDimensionless)
	requestCountM = stats.Int64(
		"request_count",
		"The number of requests that are routed to Activator",
		stats.UnitDimensionless)
	responseTimeInMsecM = stats.Float64(
		"request_latencies",
		"The response time in millisecond",
		stats.UnitMilliseconds)

	// NOTE: 0 should not be used as boundary. See
	// https://github.com/census-ecosystem/opencensus-go-exporter-stackdriver/issues/98
	defaultLatencyDistribution = view.Distribution(5, 10, 20, 40, 60, 80, 100, 150, 200, 250, 300, 350, 400, 450, 500, 600, 700, 800, 900, 1000, 2000, 5000, 10000, 20000, 50000, 100000)
)

// StatsReporter defines the interface for sending activator metrics
type StatsReporter interface {
	GetRevisionStatsReporter(ns, service, config, rev string) (RevisionStatsReporter, error)
}

// RevisionStatsReporter defines the interface for sending revision specific metrics.
type RevisionStatsReporter interface {
	ReportRequestConcurrency(v int64)
	ReportRequestCount(responseCode int)
	ReportResponseTime(responseCode int, d time.Duration)
}

// reporter holds cached metric objects to report autoscaler metrics
type reporter struct {
	ctx context.Context
}

// NewStatsReporter creates a reporter that collects and reports activator metrics
func NewStatsReporter(pod string) (StatsReporter, error) {
	ctx, err := tag.New(
		context.Background(),
		tag.Upsert(metrics.PodTagKey, pod),
		tag.Upsert(metrics.ContainerTagKey, Name),
	)
	if err != nil {
		return nil, err
	}

	// Create view to see our measurements.
	if err := view.Register(
		&view.View{
			Description: "Concurrent requests that are routed to Activator",
			Measure:     requestConcurrencyM,
			Aggregation: view.LastValue(),
			TagKeys:     append(metrics.CommonRevisionKeys, metrics.PodTagKey, metrics.ContainerTagKey),
		},
		&view.View{
			Description: "The number of requests that are routed to Activator",
			Measure:     requestCountM,
			Aggregation: view.Count(),
			TagKeys: append(metrics.CommonRevisionKeys, metrics.PodTagKey, metrics.ContainerTagKey,
				metrics.ResponseCodeKey, metrics.ResponseCodeClassKey),
		},
		&view.View{
			Description: "The response time in millisecond",
			Measure:     responseTimeInMsecM,
			Aggregation: defaultLatencyDistribution,
			TagKeys: append(metrics.CommonRevisionKeys, metrics.PodTagKey, metrics.ContainerTagKey,
				metrics.ResponseCodeKey, metrics.ResponseCodeClassKey),
		},
	); err != nil {
		return nil, err
	}

	return &reporter{ctx: ctx}, nil
}

func valueOrUnknown(v string) string {
	if v != "" {
		return v
	}
	return metricskey.ValueUnknown
}

func (r *reporter) GetRevisionStatsReporter(ns, service, config, rev string) (RevisionStatsReporter, error) {
	ctx, err := tag.New(
		r.ctx,
		tag.Upsert(metrics.NamespaceTagKey, ns),
		tag.Upsert(metrics.ServiceTagKey, valueOrUnknown(service)),
		tag.Upsert(metrics.ConfigTagKey, config),
		tag.Upsert(metrics.RevisionTagKey, rev))
	if err != nil {
		return &revisionReporter{}, err
	}

	return &revisionReporter{ctx: ctx}, nil
}

type revisionReporter struct {
	ctx context.Context
}

// ReportRequestConcurrency captures request concurrency metric with value v.
func (r *revisionReporter) ReportRequestConcurrency(v int64) {
	if r.ctx == nil {
		return
	}

	pkgmetrics.Record(r.ctx, requestConcurrencyM.M(v))
}

// ReportRequestCount captures request count.
func (r *revisionReporter) ReportRequestCount(responseCode int) {
	if r.ctx == nil {
		return
	}

	// It's safe to ignore the error as the tags are guaranteed to pass the checks in all cases.
	ctx, _ := tag.New(
		r.ctx,
		tag.Upsert(metrics.ResponseCodeKey, strconv.Itoa(responseCode)),
		tag.Upsert(metrics.ResponseCodeClassKey, responseCodeClass(responseCode)))

	pkgmetrics.Record(ctx, requestCountM.M(1))
}

// ReportResponseTime captures response time requests
func (r *revisionReporter) ReportResponseTime(responseCode int, d time.Duration) {
	if r.ctx == nil {
		return
	}

	// It's safe to ignore the error as the tags are guaranteed to pass the checks in all cases.
	ctx, _ := tag.New(
		r.ctx,
		tag.Upsert(metrics.ResponseCodeKey, strconv.Itoa(responseCode)),
		tag.Upsert(metrics.ResponseCodeClassKey, responseCodeClass(responseCode)))

	pkgmetrics.Record(ctx, responseTimeInMsecM.M(float64(d.Milliseconds())))
}

// responseCodeClass converts response code to a string of response code class.
// e.g. The response code class is "5xx" for response code 503.
func responseCodeClass(responseCode int) string {
	// Get the hundred digit of the response code and concatenate "xx".
	return strconv.Itoa(responseCode/100) + "xx"
}
