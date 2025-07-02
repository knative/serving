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

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Fedosin/libkpa/algorithm"
	"github.com/Fedosin/libkpa/api"
	"github.com/Fedosin/libkpa/metrics"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	kmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/resources"
)

// libkpaAutoscaler implements UniScaler using the libkpa library.
type libkpaAutoscaler struct {
	namespace    string
	revision     string
	metricClient kmetrics.MetricClient
	podCounter   resources.EndpointsCounter
	reporterCtx  context.Context

	// Protects access to the spec and autoscaler
	mux        sync.RWMutex
	spec       *DeciderSpec
	autoscaler *algorithm.SlidingWindowAutoscaler
}

// NewLibKPAAutoscaler creates a new UniScaler implementation using libkpa.
func NewLibKPAAutoscaler(
	reporterCtx context.Context,
	namespace, revision string,
	metricClient kmetrics.MetricClient,
	podCounter resources.EndpointsCounter,
	deciderSpec *DeciderSpec,
) (UniScaler, error) {
	la := &libkpaAutoscaler{
		namespace:    namespace,
		revision:     revision,
		metricClient: metricClient,
		podCounter:   podCounter,
		reporterCtx:  reporterCtx,
		spec:         deciderSpec,
	}

	podCount, err := podCounter.ReadyCount()
	if err != nil {
		return nil, err
	}

	if podCount > math.MaxInt32 {
		return nil, fmt.Errorf("current pods count is too high: %d", podCount)
	}

	// Create libkpa autoscaler with the initial spec
	la.autoscaler = la.createAutoscaler(deciderSpec, int32(podCount)) //nolint:gosec

	return la, nil
}

func convertSpecToAutoscalerSpec(spec *DeciderSpec) api.AutoscalerConfig {
	return api.AutoscalerConfig{
		MaxScaleUpRate:      spec.MaxScaleUpRate,
		MaxScaleDownRate:    spec.MaxScaleDownRate,
		StableWindow:        spec.StableWindow,
		ScaleDownDelay:      spec.ScaleDownDelay,
		PanicThreshold:      spec.PanicThreshold,
		TargetValue:         spec.TargetValue,
		TotalValue:          spec.TotalValue,
		TargetBurstCapacity: spec.TargetBurstCapacity,
		ActivationScale:     spec.ActivationScale,
		Reachable:           spec.Reachable,
	}
}

// Update reconfigures the UniScaler according to the DeciderSpec.
func (la *libkpaAutoscaler) Update(spec *DeciderSpec) {
	la.mux.Lock()
	defer la.mux.Unlock()

	la.spec = spec
	// Recreate the autoscaler with new spec
	la.autoscaler.Update(convertSpecToAutoscalerSpec(spec))
}

// Scale calculates the desired scale based on current statistics.
func (la *libkpaAutoscaler) Scale(logger *zap.SugaredLogger, now time.Time) ScaleResult {
	la.mux.RLock()
	defer la.mux.RUnlock()

	// Get current pod count
	currentPods, err := la.podCounter.ReadyCount()
	if err != nil {
		logger.Errorw("Failed to get ready pod count", zap.Error(err))
		return invalidSR
	}

	// Get metrics from the metric client
	metricKey := types.NamespacedName{Namespace: la.namespace, Name: la.revision}

	var stableValue, panicValue float64
	switch la.spec.ScalingMetric {
	case "rps":
		stableValue, panicValue, err = la.metricClient.StableAndPanicRPS(metricKey, now)
	default:
		stableValue, panicValue, err = la.metricClient.StableAndPanicConcurrency(metricKey, now)
	}

	if err != nil {
		if errors.Is(err, kmetrics.ErrNoData) {
			logger.Debug("No data to scale on yet")
		} else {
			logger.Errorw("Failed to obtain metrics", zap.Error(err))
		}
		return invalidSR
	}

	if currentPods > math.MaxInt32 {
		logger.Errorw("Current pods count is too high", zap.Int("currentPods", currentPods))
		return invalidSR
	}

	// Create a metric snapshot for libkpa
	snapshot := metrics.NewMetricSnapshot(
		stableValue,
		panicValue,
		int32(currentPods), //nolint:gosec
		now,
	)

	// Get scaling recommendation from libkpa
	recommendation := la.autoscaler.Scale(snapshot, now)

	if !recommendation.ScaleValid {
		return invalidSR
	}

	// Calculate excess burst capacity
	excessBCF := float64(recommendation.ExcessBurstCapacity)
	if la.spec.TargetBurstCapacity == 0 {
		excessBCF = 0
	} else if la.spec.TargetBurstCapacity > 0 {
		totCap := float64(currentPods) * la.spec.TotalValue
		excessBCF = totCap - la.spec.TargetBurstCapacity - panicValue
	}

	return ScaleResult{
		DesiredPodCount:     recommendation.DesiredPodCount,
		ExcessBurstCapacity: int32(excessBCF),
		ScaleValid:          true,
	}
}

// createAutoscaler maps DeciderSpec to libkpa AutoscalerSpec and create the sliding window autoscaler
func (la *libkpaAutoscaler) createAutoscaler(spec *DeciderSpec, podCount int32) *algorithm.SlidingWindowAutoscaler {
	return algorithm.NewSlidingWindowAutoscaler(convertSpecToAutoscalerSpec(spec), podCount)
}
