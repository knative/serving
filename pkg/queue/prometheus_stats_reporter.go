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

package queue

import (
	"errors"
	"net/http"
	"time"

	"github.com/knative/serving/pkg/autoscaler"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// ReporterReportingPeriod is the interval of time between reporting stats by queue proxy.
	ReporterReportingPeriod = time.Second

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

	// TODO(#2524): make reporting period accurate.
	operationsPerSecondGV = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_operations_per_second",
			Help: "Number of operations per second",
		},
		metricLabelNames,
	)
	proxiedOperationsPerSecondGV = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_proxied_operations_per_second",
			Help: "Number of proxied operations per second",
		},
		metricLabelNames,
	)
	averageConcurrentRequestsGV = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_average_concurrent_requests",
			Help: "Number of requests currently being handled by this pod",
		},
		metricLabelNames,
	)
	averageProxiedConcurrentRequestsGV = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_average_proxied_concurrent_requests",
			Help: "Number of proxied requests currently being handled by this pod",
		},
		metricLabelNames,
	)
)

// PrometheusStatsReporter structure represents a prometheus stats reporter.
type PrometheusStatsReporter struct {
	initialized bool
	labels      prometheus.Labels

	// An uninstrumented http.Handler used to serve stats registered by this
	// Prometheus reporter.
	Handler http.Handler
}

// NewPrometheusStatsReporter creates a reporter that collects and reports queue metrics.
func NewPrometheusStatsReporter(namespace, config, revision, pod string) (*PrometheusStatsReporter, error) {
	if len(namespace) < 1 {
		return nil, errors.New("namespace must not be empty")
	}
	if len(config) < 1 {
		return nil, errors.New("config must not be empty")
	}
	if len(revision) < 1 {
		return nil, errors.New("revision must not be empty")
	}
	if len(pod) < 1 {
		return nil, errors.New("pod must not be empty")
	}

	registry := prometheus.NewRegistry()
	err := registry.Register(operationsPerSecondGV)
	if err != nil {
		return nil, err
	}
	err = registry.Register(proxiedOperationsPerSecondGV)
	if err != nil {
		return nil, err
	}
	err = registry.Register(averageConcurrentRequestsGV)
	if err != nil {
		return nil, err
	}
	err = registry.Register(averageProxiedConcurrentRequestsGV)
	if err != nil {
		return nil, err
	}

	return &PrometheusStatsReporter{
		initialized: true,
		labels: prometheus.Labels{
			destinationNsLabel:     namespace,
			destinationConfigLabel: config,
			destinationRevLabel:    revision,
			destinationPodLabel:    pod,
		},
		Handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}, nil
}

// Report captures request metrics.
func (r *PrometheusStatsReporter) Report(stat *autoscaler.Stat) error {
	if !r.initialized {
		return errors.New("PrometheusStatsReporter is not initialized yet")
	}

	operationsPerSecondGV.With(r.labels).Set(float64(stat.RequestCount))
	proxiedOperationsPerSecondGV.With(r.labels).Set(float64(stat.ProxiedRequestCount))
	averageConcurrentRequestsGV.With(r.labels).Set(stat.AverageConcurrentRequests)
	averageProxiedConcurrentRequestsGV.With(r.labels).Set(stat.AverageProxiedConcurrentRequests)

	return nil
}
