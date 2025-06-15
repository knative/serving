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

// Package algorithm implements the KPA autoscaling algorithms.
package algorithm

import (
	"math"
	"sync"
	"time"

	"github.com/Fedosin/libkpa/api"
	"github.com/Fedosin/libkpa/maxtimewindow"
)

// SlidingWindowAutoscaler implements the sliding window autoscaling algorithm
// used by Knative's KPA (Knative Pod Autoscaler).
type SlidingWindowAutoscaler struct {
	mu sync.RWMutex

	// Configuration
	spec api.AutoscalerSpec

	// State for panic mode
	panicTime    time.Time
	maxPanicPods int32

	// Delay window for scale-down decisions
	maxTimeWindow *maxtimewindow.TimeWindow
}

const (
	scaleDownDelayGranularity = 2 * time.Second
)

// NewSlidingWindowAutoscaler creates a new sliding window autoscaler.
func NewSlidingWindowAutoscaler(spec api.AutoscalerSpec, initialScale int32) *SlidingWindowAutoscaler {
	var maxTimeWindow *maxtimewindow.TimeWindow
	if spec.ScaleDownDelay > 0 {
		maxTimeWindow = maxtimewindow.NewTimeWindow(spec.ScaleDownDelay, scaleDownDelayGranularity)
	}

	result := &SlidingWindowAutoscaler{
		spec:          spec,
		maxTimeWindow: maxTimeWindow,
	}

	// We always start in the panic mode, if the deployment is scaled up over 1 pod.
	// If the scale is 0 or 1, normal Autoscaler behavior is fine.
	// When Autoscaler restarts we lose metric history, which causes us to
	// momentarily scale down, and that is not a desired behavior.
	// Thus, we're keeping at least the current scale until we
	// accumulate enough data to make conscious decisions.
	if initialScale > 1 {
		result.maxPanicPods = initialScale
		result.panicTime = time.Now()
	}

	return result
}

// Scale calculates the desired scale based on current metrics.
func (a *SlidingWindowAutoscaler) Scale(snapshot api.MetricSnapshot, now time.Time) api.ScaleRecommendation {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Get current ready pod count
	readyPodCount := snapshot.ReadyPodCount()
	if readyPodCount == 0 {
		readyPodCount = 1 // Avoid division by zero
	}

	// Get metric values
	observedStableValue := snapshot.StableValue()
	observedPanicValue := snapshot.PanicValue()

	// If no data, return invalid recommendation
	if observedStableValue < 0 || observedPanicValue < 0 {
		return api.ScaleRecommendation{
			ScaleValid: false,
		}
	}

	// Calculate scale limits based on current pod count
	maxScaleUp := int32(math.Ceil(a.spec.MaxScaleUpRate * float64(readyPodCount)))
	maxScaleDown := int32(0)
	if a.spec.Reachable {
		maxScaleDown = int32(math.Floor(float64(readyPodCount) / a.spec.MaxScaleDownRate))
	}

	// raw pod counts calculated directly from metrics, prior to applying any rate limits.
	var rawStablePodCount, rawPanicPodCount int32

	if a.spec.TargetValue == 0 {
		// When target value is zero, any positive metric value would require infinite pods
		// So we set to a very large value that will be clamped by rate limits and max scale
		rawStablePodCount = math.MaxInt32
		rawPanicPodCount = math.MaxInt32
	} else {
		rawStablePodCount = int32(math.Ceil(observedStableValue / a.spec.TargetValue))
		rawPanicPodCount = int32(math.Ceil(observedPanicValue / a.spec.TargetValue))
	}

	// Apply scale limits
	desiredStablePodCount := min(max(rawStablePodCount, maxScaleDown), maxScaleUp)
	desiredPanicPodCount := min(max(rawPanicPodCount, maxScaleDown), maxScaleUp)

	// Apply activation scale if needed
	if a.spec.ActivationScale > 1 {
		// Activation scale should apply only when there is actual demand (i.e. raw counts > 0).
		// This prevents the activation scale from blocking scale-to-zero.
		if rawStablePodCount > 0 && a.spec.ActivationScale > desiredStablePodCount {
			desiredStablePodCount = a.spec.ActivationScale
		}
		if rawPanicPodCount > 0 && a.spec.ActivationScale > desiredPanicPodCount {
			desiredPanicPodCount = a.spec.ActivationScale
		}
	}

	// Check panic mode conditions
	isOverPanicThreshold := float64(rawPanicPodCount)/float64(readyPodCount) >= a.spec.PanicThreshold
	inPanicMode := !a.panicTime.IsZero()

	// Update panic mode state
	switch {
	case !inPanicMode && isOverPanicThreshold:
		// Enter panic mode
		a.panicTime = now
		inPanicMode = true
	case isOverPanicThreshold:
		// Extend panic mode
		a.panicTime = now
	case inPanicMode && !isOverPanicThreshold && a.panicTime.Add(a.spec.StableWindow).Before(now):
		// Exit panic mode
		a.panicTime = time.Time{}
		a.maxPanicPods = 0
		inPanicMode = false
	}

	// Determine final desired pod count
	desiredPodCount := desiredStablePodCount
	if inPanicMode {
		// Use the higher of stable or panic pod count
		if desiredPanicPodCount > desiredPodCount {
			desiredPodCount = desiredPanicPodCount
		}
		// Never scale down in panic mode
		if desiredPodCount > a.maxPanicPods {
			a.maxPanicPods = desiredPodCount
		} else {
			desiredPodCount = a.maxPanicPods
		}
	}

	// Apply scale-down delay if configured
	if a.spec.Reachable && a.maxTimeWindow != nil {
		a.maxTimeWindow.Record(now, desiredPodCount)
		desiredPodCount = a.maxTimeWindow.Current()
	}

	// Apply min/max scale bounds
	if a.spec.MinScale > 0 && desiredPodCount < a.spec.MinScale {
		desiredPodCount = a.spec.MinScale
	}
	if a.spec.MaxScale > 0 && desiredPodCount > a.spec.MaxScale {
		desiredPodCount = a.spec.MaxScale
	}

	// Calculate excess burst capacity
	excessBurstCapacity := calculateExcessBurstCapacity(
		snapshot.ReadyPodCount(),
		a.spec.TotalValue,
		a.spec.TargetBurstCapacity,
		observedPanicValue,
	)

	return api.ScaleRecommendation{
		DesiredPodCount:     desiredPodCount,
		ExcessBurstCapacity: excessBurstCapacity,
		ScaleValid:          true,
		InPanicMode:         inPanicMode,
		ObservedStableValue: observedStableValue,
		ObservedPanicValue:  observedPanicValue,
		CurrentPodCount:     snapshot.ReadyPodCount(),
	}
}

// Update reconfigures the autoscaler with a new spec.
func (a *SlidingWindowAutoscaler) Update(spec api.AutoscalerSpec) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.spec = spec

	// Update delay window if needed
	if spec.ScaleDownDelay > 0 {
		a.maxTimeWindow = maxtimewindow.NewTimeWindow(spec.ScaleDownDelay, scaleDownDelayGranularity)
	}

	return nil
}

// GetSpec returns the current autoscaler spec.
func (a *SlidingWindowAutoscaler) GetSpec() api.AutoscalerSpec {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.spec
}

// calculateExcessBurstCapacity computes the excess burst capacity.
// A negative value means the deployment doesn't have enough capacity
// to handle the target burst capacity.
func calculateExcessBurstCapacity(readyPods int32, totalValue, targetBurstCapacity, observedPanicValue float64) int32 {
	if targetBurstCapacity == 0 {
		return 0
	}
	if targetBurstCapacity < 0 {
		return -1 // Unlimited
	}

	totalCapacity := float64(readyPods) * totalValue
	excessBurstCapacity := math.Floor(totalCapacity - targetBurstCapacity - observedPanicValue)
	return int32(excessBurstCapacity)
}
