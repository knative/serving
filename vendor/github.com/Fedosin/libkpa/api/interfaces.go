/*
Copyright 2025 The libkpa Authors

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

package api

import (
	"context"
	"time"
)

// Autoscaler is the main interface for the KPA autoscaler.
type Autoscaler interface {
	// Scale calculates the desired scale based on the provided metrics and time.
	// It returns a ScaleRecommendation with the suggested pod count and other details.
	Scale(metrics MetricSnapshot, now time.Time) ScaleRecommendation

	// Update reconfigures the autoscaler with a new spec.
	Update(spec AutoscalerConfig) error

	// GetSpec returns the current autoscaler spec.
	GetSpec() AutoscalerConfig
}

// MetricClient defines the interface for retrieving metrics.
type MetricClient interface {
	// GetMetrics returns the current metric snapshot for scaling decisions.
	GetMetrics(ctx context.Context) (MetricSnapshot, error)
}

// MetricSnapshot represents a point-in-time view of metrics.
type MetricSnapshot interface {
	// StableValue returns the metric value averaged over the stable window.
	StableValue() float64

	// PanicValue returns the metric value averaged over the panic window.
	PanicValue() float64

	// ReadyPodCount returns the number of ready pods.
	ReadyPodCount() int32

	// Timestamp returns when this snapshot was taken.
	Timestamp() time.Time
}

// PodCounter provides information about pod readiness.
type PodCounter interface {
	// ReadyCount returns the number of ready pods.
	ReadyCount() (int, error)
}

// MetricCollector collects metrics from pods.
type MetricCollector interface {
	// CollectMetrics collects metrics from all pods.
	CollectMetrics(ctx context.Context, pods []string) ([]PodMetrics, error)

	// CreateSnapshot creates a metric snapshot from collected pod metrics.
	CreateSnapshot(metrics []PodMetrics, now time.Time) MetricSnapshot
}

// MetricAggregator aggregates metrics over time windows.
type MetricAggregator interface {
	// Record adds a metric value at the given time.
	Record(time time.Time, value float64)

	// WindowAverage returns the average over the configured window.
	WindowAverage(now time.Time) float64

	// IsEmpty returns true if no data exists in the window.
	IsEmpty(now time.Time) bool
}

// Reporter reports autoscaler metrics for monitoring.
type Reporter interface {
	// ReportMetrics reports the current state of the autoscaler.
	ReportMetrics(ctx context.Context, spec AutoscalerConfig, recommendation ScaleRecommendation)
}
