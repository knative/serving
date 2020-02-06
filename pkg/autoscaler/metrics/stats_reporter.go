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

package metrics

import (
	"context"

	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/serving/pkg/metrics"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	desiredPodCountM = stats.Int64(
		"desired_pods",
		"Number of pods autoscaler wants to allocate",
		stats.UnitDimensionless)
	excessBurstCapacityM = stats.Float64(
		"excess_burst_capacity",
		"Excess burst capacity overserved over the stable window",
		stats.UnitDimensionless)
	stableRequestConcurrencyM = stats.Float64(
		"stable_request_concurrency",
		"Average of requests count per observed pod over the stable window",
		stats.UnitDimensionless)
	panicRequestConcurrencyM = stats.Float64(
		"panic_request_concurrency",
		"Average of requests count per observed pod over the panic window",
		stats.UnitDimensionless)
	targetRequestConcurrencyM = stats.Float64(
		"target_concurrency_per_pod",
		"The desired number of concurrent requests for each pod",
		stats.UnitDimensionless)
	stableRPSM = stats.Float64(
		"stable_requests_per_second",
		"Average requests-per-second per observed pod over the stable window",
		stats.UnitDimensionless)
	panicRPSM = stats.Float64(
		"panic_requests_per_second",
		"Average requests-per-second per observed pod over the panic window",
		stats.UnitDimensionless)
	targetRPSM = stats.Float64(
		"target_requests_per_second",
		"The desired requests-per-second for each pod",
		stats.UnitDimensionless)
	panicM = stats.Int64(
		"panic_mode",
		"1 if autoscaler is in panic mode, 0 otherwise",
		stats.UnitDimensionless)
)

func init() {
	register()
}

func register() {
	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	if err := view.Register(
		&view.View{
			Description: "Number of pods autoscaler wants to allocate",
			Measure:     desiredPodCountM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Average of requests count over the stable window",
			Measure:     stableRequestConcurrencyM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Current excess burst capacity over average request count over the stable window",
			Measure:     excessBurstCapacityM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Average of requests count over the panic window",
			Measure:     panicRequestConcurrencyM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "The desired number of concurrent requests for each pod",
			Measure:     targetRequestConcurrencyM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "1 if autoscaler is in panic mode, 0 otherwise",
			Measure:     panicM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Average requests-per-second over the stable window",
			Measure:     stableRPSM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Average requests-per-second over the panic window",
			Measure:     panicRPSM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "The desired requests-per-second for each pod",
			Measure:     targetRPSM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
	); err != nil {
		panic(err)
	}
}

// StatsReporter defines the interface for sending autoscaler metrics
type StatsReporter interface {
	ReportDesiredPodCount(v int64)
	ReportRequestConcurrency(stable, panic, target float64)
	ReportRPS(stable, panic, target float64)
	ReportExcessBurstCapacity(v float64)
	ReportPanic(v int64)
}

// reporter holds cached metric objects to report autoscaler metrics
type reporter struct {
	ctx context.Context
}

func valueOrUnknown(v string) string {
	if v != "" {
		return v
	}
	return metricskey.ValueUnknown
}

// NewStatsReporter creates a reporter that collects and reports autoscaler metrics
func NewStatsReporter(ns, service, config, revision string) (StatsReporter, error) {
	// Our tags are static. So, we can get away with creating a single context
	// and reuse it for reporting all of our metrics. Note that service names
	// can be an empty string, so it needs a special treatment.
	ctx, err := tag.New(
		context.Background(),
		tag.Upsert(metrics.NamespaceTagKey, ns),
		tag.Upsert(metrics.ServiceTagKey, valueOrUnknown(service)),
		tag.Upsert(metrics.ConfigTagKey, config),
		tag.Upsert(metrics.RevisionTagKey, revision))
	if err != nil {
		return nil, err
	}

	return &reporter{ctx: ctx}, nil
}

// ReportDesiredPodCount captures value v for desired pod count measure.
func (r *reporter) ReportDesiredPodCount(v int64) {
	pkgmetrics.Record(r.ctx, desiredPodCountM.M(v))
}

// ReportExcessBurstCapacity captures value v for excess target burst capacity.
func (r *reporter) ReportExcessBurstCapacity(v float64) {
	pkgmetrics.Record(r.ctx, excessBurstCapacityM.M(v))
}

// ReportStableRequestConcurrency captures value v for stable request concurrency measure.
func (r *reporter) ReportRequestConcurrency(stable, panic, target float64) {
	pkgmetrics.RecordBatch(r.ctx, stableRequestConcurrencyM.M(stable),
		panicRequestConcurrencyM.M(panic), targetRequestConcurrencyM.M(target))
}

// ReportStableRPS captures value v for stable RPS measure.
func (r *reporter) ReportRPS(stable, panic, target float64) {
	pkgmetrics.RecordBatch(r.ctx, stableRPSM.M(stable), panicRPSM.M(panic),
		targetRPSM.M(target))
}

// ReportPanic captures value v for panic mode measure.
func (r *reporter) ReportPanic(v int64) {
	pkgmetrics.Record(r.ctx, panicM.M(v))
}
