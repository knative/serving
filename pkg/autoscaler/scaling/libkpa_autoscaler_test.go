/*
Copyright 2025 The Knative Authors

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

package scaling

// Note: libkpa starts in panic mode when initialScale > 1. This matches the
// behavior of the built-in autoscaler. In panic mode, the autoscaler will
// not scale down below the maximum pod count it has seen. This behavior
// affects tests that start with multiple pods.

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/serving/pkg/autoscaler/metrics"
)

type mockMetricClient struct {
	stableValue float64
	panicValue  float64
	err         error
}

func (m *mockMetricClient) StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error) {
	return m.stableValue, m.panicValue, m.err
}

func (m *mockMetricClient) StableAndPanicRPS(key types.NamespacedName, now time.Time) (float64, float64, error) {
	return m.stableValue, m.panicValue, m.err
}

type mockPodCounter struct {
	readyCount int
	err        error
}

func (m *mockPodCounter) ReadyCount() (int, error) {
	return m.readyCount, m.err
}

func (m *mockPodCounter) NotReadyCount() (int, error) {
	return 0, m.err
}

func TestLibKPAAutoscalerScale(t *testing.T) {
	logger := zap.NewNop().Sugar()
	now := time.Now()

	tests := []struct {
		name           string
		spec           *DeciderSpec
		metricClient   *mockMetricClient
		podCounter     *mockPodCounter
		wantPodCount   int32
		wantScaleValid bool
	}{
		{
			name: "scale up based on concurrency",
			spec: &DeciderSpec{
				MaxScaleUpRate:      10,
				MaxScaleDownRate:    2,
				ScalingMetric:       "concurrency",
				TargetValue:         10,
				TotalValue:          100,
				TargetBurstCapacity: 50,
				PanicThreshold:      2,
				StableWindow:        60 * time.Second,
				InitialScale:        1,
				Reachable:           true,
			},
			metricClient: &mockMetricClient{
				stableValue: 100,
				panicValue:  100,
			},
			podCounter: &mockPodCounter{
				readyCount: 5,
			},
			wantPodCount:   10,
			wantScaleValid: true,
		},
		{
			name: "scale down prevented by panic mode",
			spec: &DeciderSpec{
				MaxScaleUpRate:      10,
				MaxScaleDownRate:    2,
				ScalingMetric:       "concurrency",
				TargetValue:         10,
				TotalValue:          100,
				TargetBurstCapacity: 50,
				PanicThreshold:      2,
				StableWindow:        60 * time.Second,
				InitialScale:        1,
				Reachable:           true,
			},
			metricClient: &mockMetricClient{
				stableValue: 20,
				panicValue:  20,
			},
			podCounter: &mockPodCounter{
				readyCount: 10,
			},
			wantPodCount:   10,
			wantScaleValid: true,
		},
		{
			name: "scale down from 1 pod",
			spec: &DeciderSpec{
				MaxScaleUpRate:      10,
				MaxScaleDownRate:    2,
				ScalingMetric:       "concurrency",
				TargetValue:         10,
				TotalValue:          100,
				TargetBurstCapacity: 50,
				PanicThreshold:      2,
				StableWindow:        60 * time.Second,
				InitialScale:        1,
				Reachable:           true,
			},
			metricClient: &mockMetricClient{
				stableValue: 0,
				panicValue:  0,
			},
			podCounter: &mockPodCounter{
				readyCount: 1,
			},
			wantPodCount:   0,
			wantScaleValid: true,
		},
		{
			name: "no data returns invalid",
			spec: &DeciderSpec{
				MaxScaleUpRate:      10,
				MaxScaleDownRate:    2,
				ScalingMetric:       "concurrency",
				TargetValue:         10,
				TotalValue:          100,
				TargetBurstCapacity: 50,
				PanicThreshold:      2,
				StableWindow:        60 * time.Second,
				InitialScale:        1,
				Reachable:           true,
			},
			metricClient: &mockMetricClient{
				err: metrics.ErrNoData,
			},
			podCounter: &mockPodCounter{
				readyCount: 5,
			},
			wantScaleValid: false,
		},
		{
			name: "activation scale applied",
			spec: &DeciderSpec{
				MaxScaleUpRate:      10,
				MaxScaleDownRate:    2,
				ScalingMetric:       "concurrency",
				TargetValue:         10,
				TotalValue:          100,
				TargetBurstCapacity: 50,
				PanicThreshold:      2,
				StableWindow:        60 * time.Second,
				InitialScale:        1,
				ActivationScale:     3,
				Reachable:           true,
			},
			metricClient: &mockMetricClient{
				stableValue: 5,
				panicValue:  5,
			},
			podCounter: &mockPodCounter{
				readyCount: 0,
			},
			wantPodCount:   3, // Activation scale
			wantScaleValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			autoscaler, err := NewLibKPAAutoscaler(
				context.Background(),
				"test-namespace",
				"test-revision",
				tt.metricClient,
				tt.podCounter,
				tt.spec,
			)
			if err != nil {
				t.Fatalf("Failed to create autoscaler: %v", err)
			}

			result := autoscaler.Scale(logger, now)

			if result.ScaleValid != tt.wantScaleValid {
				t.Errorf("ScaleValid = %v, want %v", result.ScaleValid, tt.wantScaleValid)
			}

			if tt.wantScaleValid && result.DesiredPodCount != tt.wantPodCount {
				t.Errorf("DesiredPodCount = %v, want %v", result.DesiredPodCount, tt.wantPodCount)
			}
		})
	}
}

func TestLibKPAAutoscalerUpdate(t *testing.T) {
	logger := zap.NewNop().Sugar()
	now := time.Now()

	initialSpec := &DeciderSpec{
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    2,
		ScalingMetric:       "concurrency",
		TargetValue:         10,
		TotalValue:          100,
		TargetBurstCapacity: 50,
		PanicThreshold:      3,
		StableWindow:        60 * time.Second,
		InitialScale:        1,
		Reachable:           true,
	}

	metricClient := &mockMetricClient{
		stableValue: 50,
		panicValue:  50,
	}

	// Start with 1 pod to avoid panic mode
	podCounter := &mockPodCounter{
		readyCount: 1,
	}

	autoscaler, err := NewLibKPAAutoscaler(
		context.Background(),
		"test-namespace",
		"test-revision",
		metricClient,
		podCounter,
		initialSpec,
	)
	if err != nil {
		t.Fatalf("Failed to create autoscaler: %v", err)
	}

	// Initial scale
	result := autoscaler.Scale(logger, now)
	if result.DesiredPodCount != 5 {
		t.Errorf("Initial DesiredPodCount = %v, want 5", result.DesiredPodCount)
	}

	// Update pod counter to reflect the scale up
	podCounter.readyCount = 5

	// Update spec with new target that would require scale up
	updatedSpec := *initialSpec
	updatedSpec.TargetValue = 5 // Lower target means more pods needed
	autoscaler.Update(&updatedSpec)

	// Scale with updated spec - should scale up
	result = autoscaler.Scale(logger, now)
	if result.DesiredPodCount != 10 {
		t.Errorf("Updated DesiredPodCount = %v, want 10", result.DesiredPodCount)
	}
}

// Test that verifies update behavior when not in panic mode
func TestLibKPAAutoscalerUpdateScaleDown(t *testing.T) {
	logger := zap.NewNop().Sugar()
	now := time.Now()

	initialSpec := &DeciderSpec{
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    2,
		ScalingMetric:       "concurrency",
		TargetValue:         10,
		TotalValue:          100,
		TargetBurstCapacity: 50,
		PanicThreshold:      3,
		StableWindow:        60 * time.Second,
		InitialScale:        1,
		Reachable:           true,
	}

	// Start with low metrics
	metricClient := &mockMetricClient{
		stableValue: 10,
		panicValue:  10,
	}

	// Start with 1 pod
	podCounter := &mockPodCounter{
		readyCount: 1,
	}

	autoscaler, err := NewLibKPAAutoscaler(
		context.Background(),
		"test-namespace",
		"test-revision",
		metricClient,
		podCounter,
		initialSpec,
	)
	if err != nil {
		t.Fatalf("Failed to create autoscaler: %v", err)
	}

	// Initial scale - should stay at 1
	result := autoscaler.Scale(logger, now)
	if result.DesiredPodCount != 1 {
		t.Errorf("Initial DesiredPodCount = %v, want 1", result.DesiredPodCount)
	}

	// Update spec with higher target (requires fewer pods)
	updatedSpec := *initialSpec
	updatedSpec.TargetValue = 20
	autoscaler.Update(&updatedSpec)

	// Scale with updated spec - should still be 1 (or could scale to 0)
	result = autoscaler.Scale(logger, now)
	// With metrics=10 and target=20, we need 0.5 pods, which rounds up to 1
	if result.DesiredPodCount > 1 {
		t.Errorf("Updated DesiredPodCount = %v, want <= 1", result.DesiredPodCount)
	}
}

// Add a test for panic mode behavior
func TestLibKPAAutoscalerPanicMode(t *testing.T) {
	logger := zap.NewNop().Sugar()
	now := time.Now()

	spec := &DeciderSpec{
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    2,
		ScalingMetric:       "concurrency",
		TargetValue:         10,
		TotalValue:          100,
		TargetBurstCapacity: 50,
		PanicThreshold:      2,
		StableWindow:        60 * time.Second,
		InitialScale:        1,
		Reachable:           true,
	}

	metricClient := &mockMetricClient{
		stableValue: 20, // Low load that would normally scale down
		panicValue:  20,
	}

	// Start with 5 pods - should trigger panic mode
	podCounter := &mockPodCounter{
		readyCount: 5,
	}

	autoscaler, err := NewLibKPAAutoscaler(
		context.Background(),
		"test-namespace",
		"test-revision",
		metricClient,
		podCounter,
		spec,
	)
	if err != nil {
		t.Fatalf("Failed to create autoscaler: %v", err)
	}

	// Should maintain initial scale of 5 due to panic mode
	result := autoscaler.Scale(logger, now)
	if !result.ScaleValid {
		t.Fatal("Expected valid scale result")
	}
	if result.DesiredPodCount != 5 {
		t.Errorf("DesiredPodCount = %v, want 5 (should maintain initial scale in panic mode)", result.DesiredPodCount)
	}

	// Even with higher load, it should not scale down from initial
	metricClient.stableValue = 40
	metricClient.panicValue = 40
	result = autoscaler.Scale(logger, now)
	if result.DesiredPodCount != 5 {
		t.Errorf("DesiredPodCount = %v, want 5 (should maintain at least initial scale)", result.DesiredPodCount)
	}

	// But it should scale up if needed
	metricClient.stableValue = 100
	metricClient.panicValue = 100
	result = autoscaler.Scale(logger, now)
	if result.DesiredPodCount != 10 {
		t.Errorf("DesiredPodCount = %v, want 10 (should scale up in panic mode)", result.DesiredPodCount)
	}
}
