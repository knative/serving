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

package algorithm

import (
	"time"

	"github.com/Fedosin/libkpa/api"
)

// PanicModeCalculator handles panic mode calculations for the autoscaler.
type PanicModeCalculator struct {
	config *api.AutoscalerConfig
}

// NewPanicModeCalculator creates a new panic mode calculator.
func NewPanicModeCalculator(config *api.AutoscalerConfig) *PanicModeCalculator {
	return &PanicModeCalculator{
		config: config,
	}
}

// CalculatePanicWindow calculates the panic window duration based on the stable window
// and panic window percentage.
func (p *PanicModeCalculator) CalculatePanicWindow() time.Duration {
	return time.Duration(float64(p.config.StableWindow) * p.config.PanicWindowPercentage / 100.0)
}

// ShouldEnterPanicMode determines if the autoscaler should enter panic mode
// based on the current load and capacity.
func (p *PanicModeCalculator) ShouldEnterPanicMode(desiredPodCount, currentPodCount float64) bool {
	if currentPodCount == 0 {
		return false
	}
	// Enter panic mode if desired pods divided by current pods exceeds the panic threshold
	return desiredPodCount/currentPodCount >= p.config.PanicThreshold
}

// ShouldExitPanicMode determines if the autoscaler should exit panic mode.
func (p *PanicModeCalculator) ShouldExitPanicMode(panicStartTime time.Time, now time.Time, isOverThreshold bool) bool {
	// Exit panic mode if:
	// 1. We're no longer over the panic threshold, AND
	// 2. A full stable window has passed since we were last over the threshold
	if !isOverThreshold && panicStartTime.Add(p.config.StableWindow).Before(now) {
		return true
	}
	return false
}

// CalculateDesiredPods calculates the desired pod count considering panic mode.
func (p *PanicModeCalculator) CalculateDesiredPods(stableDesired, panicDesired int32, inPanicMode bool, maxPanicPods int32) int32 {
	if !inPanicMode {
		return stableDesired
	}

	// In panic mode, use the higher of stable or panic desired count
	desired := stableDesired
	if panicDesired > desired {
		desired = panicDesired
	}

	// Never scale down in panic mode
	if desired < maxPanicPods {
		desired = maxPanicPods
	}

	return desired
}
