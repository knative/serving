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
	"sync"

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

	// Connection health metrics
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
		"Total number of 502 responses produced due to transport errors while targeting healthy backends",
		stats.UnitDimensionless)
	breakerPendingRequestsM = stats.Int64(
		"breaker_pending_requests",
		"Current number of requests waiting to acquire a podTracker",
		stats.UnitDimensionless)

	// Pod health metrics
	podQuarantinesM = stats.Int64(
		"pod_quarantines",
		"Current number of pods in quarantine state",
		stats.UnitDimensionless)
	// Event counters
	tcpPingFailuresTotalM = stats.Int64(
		"tcp_ping_failures_total",
		"Total number of TCP ping failures that resulted in pod quarantine",
		stats.UnitDimensionless)
	immediate502sTotalM = stats.Int64(
		"instant_502_quarantines_total",
		"Total number of immediate 502 responses that resulted in pod quarantine",
		stats.UnitDimensionless)

	// New counter-based metrics (preferred over gauges)
	podQuarantineEntriesM = stats.Int64(
		"pod_quarantine_entries_total",
		"Total number of times pods entered quarantine state",
		stats.UnitDimensionless)
	podQuarantineExitsM = stats.Int64(
		"pod_quarantine_exits_total",
		"Total number of times pods exited quarantine state",
		stats.UnitDimensionless)
	pendingRequestStartsM = stats.Int64(
		"pending_request_starts_total",
		"Total number of requests that started pending for pod trackers",
		stats.UnitDimensionless)
	pendingRequestCompletesM = stats.Int64(
		"pending_request_completes_total",
		"Total number of requests that completed pending for pod trackers",
		stats.UnitDimensionless)

	// Proxy start latency metric
	proxyStartLatencyM = stats.Float64(
		"proxy_start_latency_ms",
		"Time in milliseconds from request entry to successful proxy start (pod tracker acquisition)",
		stats.UnitMilliseconds)

	// Proxy queue time threshold breach metrics
	proxyQueueTimeWarningM = stats.Int64(
		"proxy_queue_time_warning_total",
		"Total number of requests that exceeded 4s waiting to proxy (warning threshold)",
		stats.UnitDimensionless)
	proxyQueueTimeErrorM = stats.Int64(
		"proxy_queue_time_error_total",
		"Total number of requests that exceeded 60s waiting to proxy (error threshold)",
		stats.UnitDimensionless)
	proxyQueueTimeCriticalM = stats.Int64(
		"proxy_queue_time_critical_total",
		"Total number of requests that exceeded 3m waiting to proxy (critical threshold)",
		stats.UnitDimensionless)

	// NOTE: 0 should not be used as boundary. See
	// https://github.com/census-ecosystem/opencensus-go-exporter-stackdriver/issues/98
	defaultLatencyDistribution = view.Distribution(5, 10, 20, 40, 60, 80, 100, 150, 200, 250, 300, 350, 400, 450, 500, 600, 700, 800, 900, 1000, 2000, 5000, 10000, 20000, 50000, 100000)

	// registerOnce ensures metrics are only registered once
	registerOnce sync.Once
)

func init() {
	registerOnce.Do(register)
}

// RecordPendingRequest increments the pending requests metric
func RecordPendingRequest(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, breakerPendingRequestsM.M(1))
}

// RecordTransportFailure increments transport failures counter
func RecordTransportFailure(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, transportFailuresM.M(1))
}

// RecordHealthyTargetTimeout increments timeout counter for healthy targets
func RecordHealthyTargetTimeout(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, healthyTargetTimeoutsM.M(1))
}

// RecordHealthyTarget502 increments 502 counter for healthy targets
func RecordHealthyTarget502(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, healthyTarget502sM.M(1))
}

// RecordPendingRequestComplete decrements the pending requests metric
func RecordPendingRequestComplete(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, breakerPendingRequestsM.M(-1))
}

// RecordPodQuarantineChange updates the pod quarantine gauge metric
func RecordPodQuarantineChange(ctx context.Context, delta int64) {
	pkgmetrics.RecordBatch(ctx, podQuarantinesM.M(delta))
}

// RecordTCPPingFailureEvent increments the TCP ping failure counter
func RecordTCPPingFailureEvent(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, tcpPingFailuresTotalM.M(1))
}

// RecordImmediate502Event increments the immediate-502 quarantine counter
func RecordImmediate502Event(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, immediate502sTotalM.M(1))
}

// New counter-based recording functions (preferred over gauge-based ones)

// RecordPodQuarantineEntry increments the pod quarantine entry counter
func RecordPodQuarantineEntry(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, podQuarantineEntriesM.M(1))
}

// RecordPodQuarantineExit increments the pod quarantine exit counter
func RecordPodQuarantineExit(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, podQuarantineExitsM.M(1))
}

// RecordPendingRequestStart increments the pending request start counter
func RecordPendingRequestStart(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, pendingRequestStartsM.M(1))
}

// RecordPendingRequestCompleted increments the pending request complete counter
func RecordPendingRequestCompleted(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, pendingRequestCompletesM.M(1))
}

// RecordProxyStartLatency records the time taken to successfully start proxying a request
func RecordProxyStartLatency(ctx context.Context, latencyMs float64) {
	pkgmetrics.RecordBatch(ctx, proxyStartLatencyM.M(latencyMs))
}

// RecordProxyQueueTimeWarning increments the warning threshold breach counter (>4s)
func RecordProxyQueueTimeWarning(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, proxyQueueTimeWarningM.M(1))
}

// RecordProxyQueueTimeError increments the error threshold breach counter (>60s)
func RecordProxyQueueTimeError(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, proxyQueueTimeErrorM.M(1))
}

// RecordProxyQueueTimeCritical increments the critical threshold breach counter (>3m)
func RecordProxyQueueTimeCritical(ctx context.Context) {
	pkgmetrics.RecordBatch(ctx, proxyQueueTimeCriticalM.M(1))
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
			Description: "Total number of 502 responses produced due to transport errors while targeting healthy backends",
			Measure:     healthyTarget502sM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Current number of requests waiting to acquire a podTracker",
			Measure:     breakerPendingRequestsM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		// Pod health metrics views
		&view.View{
			Description: "Current number of pods in quarantine state",
			Measure:     podQuarantinesM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of TCP ping failures that resulted in pod quarantine",
			Measure:     tcpPingFailuresTotalM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of immediate 502 responses that resulted in pod quarantine",
			Measure:     immediate502sTotalM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		// New counter-based metric views (preferred over gauges)
		&view.View{
			Description: "Total number of times pods entered quarantine state",
			Measure:     podQuarantineEntriesM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of times pods exited quarantine state",
			Measure:     podQuarantineExitsM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of requests that started pending for pod trackers",
			Measure:     pendingRequestStartsM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of requests that completed pending for pod trackers",
			Measure:     pendingRequestCompletesM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Time in milliseconds from request entry to successful proxy start (pod tracker acquisition)",
			Measure:     proxyStartLatencyM,
			Aggregation: defaultLatencyDistribution,
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		// Proxy queue time threshold breach metrics
		&view.View{
			Description: "Total number of requests that exceeded 4s waiting to proxy (warning threshold)",
			Measure:     proxyQueueTimeWarningM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of requests that exceeded 60s waiting to proxy (error threshold)",
			Measure:     proxyQueueTimeErrorM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
		&view.View{
			Description: "Total number of requests that exceeded 3m waiting to proxy (critical threshold)",
			Measure:     proxyQueueTimeCriticalM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{metrics.PodKey, metrics.ContainerKey},
		},
	); err != nil {
		panic(err)
	}
}
