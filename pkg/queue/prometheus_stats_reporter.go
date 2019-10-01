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

package queue

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"knative.dev/serving/pkg/autoscaler"
)

const (
	destinationNsLabel     = "destination_namespace"
	destinationConfigLabel = "destination_configuration"
	destinationRevLabel    = "destination_revision"
	destinationPodLabel    = "destination_pod"
)

var (
	metricLabelNames = []string{
		destinationNsLabel,
		destinationConfigLabel,
		destinationRevLabel,
		destinationPodLabel,
	}

	// For backwards compatibility, the name is kept as `operations_per_second`.
	requestsPerSecondGV = newGV(
		"queue_requests_per_second",
		"Number of requests per second")
	proxiedRequestsPerSecondGV = newGV(
		"queue_proxied_operations_per_second",
		"Number of proxied requests per second")
	averageConcurrentRequestsGV = newGV(
		"queue_average_concurrent_requests",
		"Number of requests currently being handled by this pod")
	averageProxiedConcurrentRequestsGV = newGV(
		"queue_average_proxied_concurrent_requests",
		"Number of proxied requests currently being handled by this pod")
)

func newGV(n, h string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: n, Help: h},
		metricLabelNames,
	)
}

// PrometheusStatsReporter structure represents a prometheus stats reporter.
type PrometheusStatsReporter struct {
	handler         http.Handler
	reportingPeriod time.Duration

	requestsPerSecond                prometheus.Gauge
	proxiedRequestsPerSecond         prometheus.Gauge
	averageConcurrentRequests        prometheus.Gauge
	averageProxiedConcurrentRequests prometheus.Gauge
}

// NewPrometheusStatsReporter creates a reporter that collects and reports queue metrics.
func NewPrometheusStatsReporter(namespace, config, revision, pod string, reportingPeriod time.Duration) (*PrometheusStatsReporter, error) {
	if namespace == "" {
		return nil, errors.New("namespace must not be empty")
	}
	if config == "" {
		return nil, errors.New("config must not be empty")
	}
	if revision == "" {
		return nil, errors.New("revision must not be empty")
	}
	if pod == "" {
		return nil, errors.New("pod must not be empty")
	}

	registry := prometheus.NewRegistry()
	for _, gv := range []*prometheus.GaugeVec{requestsPerSecondGV, proxiedRequestsPerSecondGV, averageConcurrentRequestsGV, averageProxiedConcurrentRequestsGV} {
		if err := registry.Register(gv); err != nil {
			return nil, fmt.Errorf("register metric failed: %v", err)
		}
	}

	labels := prometheus.Labels{
		destinationNsLabel:     namespace,
		destinationConfigLabel: config,
		destinationRevLabel:    revision,
		destinationPodLabel:    pod,
	}

	return &PrometheusStatsReporter{
		handler:         promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		reportingPeriod: reportingPeriod,

		requestsPerSecond:                requestsPerSecondGV.With(labels),
		proxiedRequestsPerSecond:         proxiedRequestsPerSecondGV.With(labels),
		averageConcurrentRequests:        averageConcurrentRequestsGV.With(labels),
		averageProxiedConcurrentRequests: averageProxiedConcurrentRequestsGV.With(labels),
	}, nil
}

// Report captures request metrics.
func (r *PrometheusStatsReporter) Report(stat autoscaler.Stat) {
	// Requests per second is a rate over time while concurrency is not.
	r.requestsPerSecond.Set(stat.RequestCount / r.reportingPeriod.Seconds())
	r.proxiedRequestsPerSecond.Set(stat.ProxiedRequestCount / r.reportingPeriod.Seconds())
	r.averageConcurrentRequests.Set(stat.AverageConcurrentRequests)
	r.averageProxiedConcurrentRequests.Set(stat.AverageProxiedConcurrentRequests)
}

// Handler returns an uninstrumented http.Handler used to serve stats registered by this
// PrometheusStatsReporter.
func (r *PrometheusStatsReporter) Handler() http.Handler {
	return r.handler
}
