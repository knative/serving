/*
Copyright 2020 The Knative Authors

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

package handler

import (
	"context"

	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/metrics"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	requestConcurrencyM = stats.Float64(
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

	// New metrics for quarantine/connection diagnostics
	healthyConnLatencyM = stats.Float64(
		"healthy_connection_latency_ms",
		"Latency in ms to establish a connection to a healthy target (RoundTrip time)",
		stats.UnitMilliseconds)
	transportFailuresM = stats.Int64(
		"transport_failures_total",
		"Total number of transport-level failures during proxying",
		stats.UnitDimensionless)
	healthyTargetTimeoutsM = stats.Int64(
		"healthy_target_connection_timeouts_total",
		"Total number of connection timeouts observed while targeting healthy backends",
		stats.UnitDimensionless)
	healthyTarget502sM = stats.Int64(
		"healthy_target_502_total",
		"Current number of 502 responses produced due to transport errors while targeting healthy backends",
		stats.UnitDimensionless)
	breakerPendingRequestsM = stats.Int64(
		"breaker_pending_requests",
		"Current number of requests waiting to acquire a podTracker",
		stats.UnitDimensionless)

	// NOTE: 0 should not be used as boundary. See
	// https://github.com/census-ecosystem/opencensus-go-exporter-stackdriver/issues/98
	defaultLatencyDistribution = view.Distribution(5, 10, 20, 40, 60, 80, 100, 150, 200, 250, 300, 350, 400, 450, 500, 600, 700, 800, 900, 1000, 2000, 5000, 10000, 20000, 50000, 100000)
)

func init() {
	register()
}

// RecordPendingRequest increments the pending requests metric
func RecordPendingRequest(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, breakerPendingRequestsM.M(1))
}

// RecordPendingRequestComplete decrements the pending requests metric
func RecordPendingRequestComplete(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, breakerPendingRequestsM.M(-1))
}

func register() {
	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	if err := pkgmetrics.RegisterResourceView(
		&view.View{
			Description: "Concurrent requests that are routed to Activator",
			Measure:     requestConcurrencyM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "The number of requests that are routed to Activator",
			Measure:     requestCountM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey, metrics.ResponseCodeKey, metrics.ResponseCodeClassKey},
		},
		&view.View{
			Description: "The response time in millisecond",
			Measure:     responseTimeInMsecM,
			Aggregation: defaultLatencyDistribution,
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey, metrics.ResponseCodeKey, metrics.ResponseCodeClassKey},
		},
		// New views for quarantine/connection metrics
		&view.View{
			Description: "Latency in ms to establish a connection to a healthy target (RoundTrip time)",
			Measure:     healthyConnLatencyM,
			Aggregation: defaultLatencyDistribution,
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of transport-level failures during proxying",
			Measure:     transportFailuresM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of connection timeouts observed while targeting healthy backends",
			Measure:     healthyTargetTimeoutsM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Current number of 502 responses produced due to transport errors while targeting healthy backends",
			Measure:     healthyTarget502sM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Current number of requests waiting to acquire a podTracker",
			Measure:     breakerPendingRequestsM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
	); err != nil {
		panic(err)
	}
}
