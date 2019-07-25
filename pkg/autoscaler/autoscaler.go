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

package autoscaler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/resources"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// Autoscaler stores current state of an instance of an autoscaler.
type Autoscaler struct {
	namespace    string
	revision     string
	metricClient MetricClient
	podCounter   resources.ReadyPodCounter
	reporter     StatsReporter

	// State in panic mode. Carries over multiple Scale calls. Guarded
	// by the stateMux.
	stateMux     sync.Mutex
	panicTime    *time.Time
	maxPanicPods int32

	// specMux guards the current DeciderSpec.
	specMux     sync.RWMutex
	deciderSpec DeciderSpec
}

// New creates a new instance of autoscaler
func New(
	namespace string,
	revision string,
	metricClient MetricClient,
	podCounter resources.ReadyPodCounter,
	deciderSpec DeciderSpec,
	reporter StatsReporter) (*Autoscaler, error) {
	if podCounter == nil {
		return nil, errors.New("'podCounter' must not be nil")
	}
	if reporter == nil {
		return nil, errors.New("stats reporter must not be nil")
	}

	// We always start in the panic mode, if the deployment is scaled up over 1 pod.
	// If the scale is 0 or 1, normal Autoscaler behavior is fine.
	// When Autoscaler restarts we lose metric history, which causes us to
	// momentarily scale down, and that is not a desired behaviour.
	// Thus, we're keeping at least the current scale until we
	// accumulate enough data to make conscious decisions.
	curC, err := podCounter.ReadyCount()
	if err != nil {
		return nil, fmt.Errorf("initial pod count failed: %v", err)
	}
	var pt *time.Time
	if curC > 1 {
		pt = ptr.Time(time.Now())
		// A new instance of autoscaler is created in panic mode.
		reporter.ReportPanic(1)
	} else {
		reporter.ReportPanic(0)
	}

	return &Autoscaler{
		namespace:    namespace,
		revision:     revision,
		metricClient: metricClient,
		podCounter:   podCounter,
		deciderSpec:  deciderSpec,
		reporter:     reporter,

		panicTime:    pt,
		maxPanicPods: int32(curC),
	}, nil
}

// Update reconfigures the UniScaler according to the DeciderSpec.
func (a *Autoscaler) Update(deciderSpec DeciderSpec) error {
	a.specMux.Lock()
	defer a.specMux.Unlock()

	a.deciderSpec = deciderSpec
	return nil
}

// Scale calculates the desired scale based on current statistics given the current time.
// desiredPodCount is the calculated pod count the autoscaler would like to set.
// validScale signifies whether the desiredPodCount should be applied or not.
func (a *Autoscaler) Scale(ctx context.Context, now time.Time) (desiredPodCount int32, excessBC int32, validScale bool) {
	logger := logging.FromContext(ctx)

	spec := a.currentSpec()
	originalReadyPodsCount, err := a.podCounter.ReadyCount()
	// If the error is NotFound, then presume 0.
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Errorw("Failed to get Endpoints via K8S Lister", zap.Error(err))
		return 0, 0, false
	}
	// Use 1 if there are zero current pods.
	readyPodsCount := math.Max(1, float64(originalReadyPodsCount))

	metricKey := types.NamespacedName{Namespace: a.namespace, Name: a.revision}
	observedStableConcurrency, observedPanicConcurrency, err := a.metricClient.StableAndPanicConcurrency(metricKey, now)
	if err != nil {
		if err == ErrNoData {
			logger.Debug("No data to scale on yet")
		} else {
			logger.Errorw("Failed to obtain metrics", zap.Error(err))
		}
		return 0, 0, false
	}

	maxScaleUp := spec.MaxScaleUpRate * readyPodsCount
	desiredStablePodCount := int32(math.Min(math.Ceil(observedStableConcurrency/spec.TargetConcurrency), maxScaleUp))
	desiredPanicPodCount := int32(math.Min(math.Ceil(observedPanicConcurrency/spec.TargetConcurrency), maxScaleUp))

	a.reporter.ReportStableRequestConcurrency(observedStableConcurrency)
	a.reporter.ReportPanicRequestConcurrency(observedPanicConcurrency)
	a.reporter.ReportTargetRequestConcurrency(spec.TargetConcurrency)

	logger.Debugw(fmt.Sprintf("Observed average %0.3f concurrency, targeting %0.3f.",
		observedStableConcurrency, spec.TargetConcurrency),
		zap.String("concurrency", "stable"))
	logger.Debugw(fmt.Sprintf("Observed average %0.3f concurrency, targeting %0.3f.",
		observedPanicConcurrency, spec.TargetConcurrency),
		zap.String("concurrency", "panic"))

	isOverPanicThreshold := observedPanicConcurrency/readyPodsCount >= spec.PanicThreshold

	a.stateMux.Lock()
	defer a.stateMux.Unlock()
	if a.panicTime == nil && isOverPanicThreshold {
		// Begin panicking when we cross the concurrency threshold in the panic window.
		logger.Info("PANICKING")
		a.panicTime = &now
		a.reporter.ReportPanic(1)
	} else if a.panicTime != nil && !isOverPanicThreshold && a.panicTime.Add(spec.StableWindow).Before(now) {
		// Stop panicking after the surge has made its way into the stable metric.
		logger.Info("Un-panicking.")
		a.panicTime = nil
		a.maxPanicPods = 0
		a.reporter.ReportPanic(0)
	}

	if a.panicTime != nil {
		logger.Debug("Operating in panic mode.")
		// We do not scale down while in panic mode. Only increases will be applied.
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", originalReadyPodsCount, desiredPanicPodCount)
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		desiredPodCount = a.maxPanicPods
	} else {
		logger.Debug("Operating in stable mode.")
		desiredPodCount = desiredStablePodCount
	}

	// Compute the excess burst capacity based on stable concurrency for now, since we don't want to
	// be making knee-jerk decisions about Activator in the request path. Negative EBC means
	// that the deployment does not have enough capacity to serve the desired burst off hand.
	// EBC = TotCapacity - Cur#ReqInFlight - TargetBurstCapacity
	excessBC = int32(-1)
	switch {
	case a.deciderSpec.TargetBurstCapacity == 0:
		excessBC = 0
	case a.deciderSpec.TargetBurstCapacity >= 0:
		excessBC = int32(math.Floor(float64(originalReadyPodsCount)*a.deciderSpec.TotalConcurrency - observedStableConcurrency -
			a.deciderSpec.TargetBurstCapacity))
		logger.Debugf("PodCount=%v TotalConc=%v ObservedStableConc=%v TargetBC=%v ExcessBC=%v",
			originalReadyPodsCount,
			a.deciderSpec.TotalConcurrency,
			observedStableConcurrency, a.deciderSpec.TargetBurstCapacity, excessBC)
	}
	a.reporter.ReportExcessBurstCapacity(float64(excessBC))

	a.reporter.ReportDesiredPodCount(int64(desiredPodCount))
	return desiredPodCount, excessBC, true
}

func (a *Autoscaler) currentSpec() DeciderSpec {
	a.specMux.RLock()
	defer a.specMux.RUnlock()
	return a.deciderSpec
}
