/*
Copyright 2026 The Knative Authors

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

package net

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// mockQueueDepthProvider implements QueueDepthProvider for testing.
type mockQueueDepthProvider struct {
	depths map[string]int
}

func (m *mockQueueDepthProvider) QueueDepth() map[string]int {
	return m.depths
}

func TestNewThrottlerMetrics(t *testing.T) {
	provider := &mockQueueDepthProvider{
		depths: map[string]int{
			"default/rev1": 5,
			"default/rev2": 10,
		},
	}

	// Create a meter provider for testing
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	metrics, err := newThrottlerMetrics(mp, provider)
	if err != nil {
		t.Fatalf("newThrottlerMetrics() failed: %v", err)
	}

	if metrics == nil {
		t.Fatal("newThrottlerMetrics() returned nil metrics")
	}

	// Collect metrics to trigger the callback
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify we got the expected metrics
	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("Expected scope metrics, got none")
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "kn.activator.queue.depth" {
				found = true
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("Expected Gauge[int64], got %T", m.Data)
				}
				if len(gauge.DataPoints) != 2 {
					t.Errorf("Expected 2 data points, got %d", len(gauge.DataPoints))
				}
			}
		}
	}

	if !found {
		t.Error("kn.activator.queue.depth metric not found")
	}

	// Test shutdown
	if err := metrics.Shutdown(); err != nil {
		t.Errorf("Shutdown() failed: %v", err)
	}
}

func TestNewThrottlerMetricsWithNilProvider(t *testing.T) {
	provider := &mockQueueDepthProvider{
		depths: map[string]int{},
	}

	// Test with nil MeterProvider - should use default
	metrics, err := newThrottlerMetrics(nil, provider)
	if err != nil {
		t.Fatalf("newThrottlerMetrics(nil, provider) failed: %v", err)
	}

	if metrics == nil {
		t.Fatal("newThrottlerMetrics() returned nil metrics")
	}

	// Shutdown should work even with default provider
	if err := metrics.Shutdown(); err != nil {
		t.Errorf("Shutdown() failed: %v", err)
	}
}

func TestThrottlerMetricsShutdownNilObserver(t *testing.T) {
	m := &throttlerMetrics{
		queueDepthObserver: nil,
	}

	// Should not error when observer is nil
	if err := m.Shutdown(); err != nil {
		t.Errorf("Shutdown() with nil observer failed: %v", err)
	}
}

func TestThrottlerMetricsEmptyQueueDepth(t *testing.T) {
	provider := &mockQueueDepthProvider{
		depths: map[string]int{},
	}

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	metrics, err := newThrottlerMetrics(mp, provider)
	if err != nil {
		t.Fatalf("newThrottlerMetrics() failed: %v", err)
	}

	// Collect metrics
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// When there are no data points, the metric may not appear in the output.
	// This is expected behavior for observable gauges with no observations.
	// Just verify we can collect without error and shutdown cleanly.
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "kn.activator.queue.depth" {
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("Expected Gauge[int64], got %T", m.Data)
				}
				if len(gauge.DataPoints) != 0 {
					t.Errorf("Expected 0 data points for empty provider, got %d", len(gauge.DataPoints))
				}
			}
		}
	}

	if err := metrics.Shutdown(); err != nil {
		t.Errorf("Shutdown() failed: %v", err)
	}
}
