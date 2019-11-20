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
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/resources"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// Autoscaler stores current state of an instance of an autoscaler.
type Autoscaler struct {
	namespace    string
	revision     string
	metricClient MetricClient
	lister       corev1listers.EndpointsLister
	reporter     StatsReporter

	// State in panic mode. Carries over multiple Scale calls. Guarded
	// by the stateMux.
	stateMux     sync.Mutex
	panicTime    time.Time
	maxPanicPods int32

	// specMux guards the current DeciderSpec and the PodCounter.
	specMux     sync.RWMutex
	deciderSpec *DeciderSpec
	podCounter  resources.ReadyPodCounter
}

// New creates a new instance of autoscaler
func New(
	namespace string,
	revision string,
	metricClient MetricClient,
	lister corev1listers.EndpointsLister,
	deciderSpec *DeciderSpec,
	reporter StatsReporter) (*Autoscaler, error) {
	if lister == nil {
		return nil, errors.New("'lister' must not be nil")
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
	podCounter := resources.NewScopedEndpointsCounter(lister,
		namespace, deciderSpec.ServiceName)
	curC, err := podCounter.ReadyCount()
	if err != nil {
		// This always happens on new revision creation, since decider
		// is reconciled before SKS has even chance of creating the service/endpoints.
		curC = 0
	}
	var pt time.Time
	if curC > 1 {
		pt = time.Now()
		// A new instance of autoscaler is created in panic mode.
		reporter.ReportPanic(1)
	} else {
		reporter.ReportPanic(0)
	}

	return &Autoscaler{
		namespace:    namespace,
		revision:     revision,
		metricClient: metricClient,
		lister:       lister,
		reporter:     reporter,

		deciderSpec: deciderSpec,
		podCounter:  podCounter,

		panicTime:    pt,
		maxPanicPods: int32(curC),
	}, nil
}

// Update reconfigures the UniScaler according to the DeciderSpec.
func (a *Autoscaler) Update(deciderSpec *DeciderSpec) error {
	a.specMux.Lock()
	defer a.specMux.Unlock()

	// Update the podCounter if service name changes.
	if deciderSpec.ServiceName != a.deciderSpec.ServiceName {
		a.podCounter = resources.NewScopedEndpointsCounter(a.lister, a.namespace,
			deciderSpec.ServiceName)
	}
	a.deciderSpec = deciderSpec
	return nil
}

// Scale calculates the desired scale based on current statistics given the current time.
// desiredPodCount is the calculated pod count the autoscaler would like to set.
// validScale signifies whether the desiredPodCount should be applied or not.
func (a *Autoscaler) Scale(ctx context.Context, now time.Time) (desiredPodCount int32, excessBC int32, validScale bool) {
	logger := logging.FromContext(ctx)

	spec, podCounter := a.currentSpecAndPC()
	originalReadyPodsCount, err := podCounter.ReadyCount()
	// If the error is NotFound, then presume 0.
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Errorw("Failed to get Endpoints via K8S Lister", zap.Error(err))
		return 0, 0, false
	}
	// Use 1 if there are zero current pods.
	readyPodsCount := math.Max(1, float64(originalReadyPodsCount))

	metricKey := types.NamespacedName{Namespace: a.namespace, Name: a.revision}

	metricName := spec.ScalingMetric
	var observedStableValue, observedPanicValue float64
	switch spec.ScalingMetric {
	case autoscaling.RPS:
		observedStableValue, observedPanicValue, err = a.metricClient.StableAndPanicRPS(metricKey, now)
		a.reporter.ReportStableRPS(observedStableValue)
		a.reporter.ReportPanicRPS(observedPanicValue)
		a.reporter.ReportTargetRPS(spec.TargetValue)
	default:
		metricName = autoscaling.Concurrency // concurrency is used by default
		observedStableValue, observedPanicValue, err = a.metricClient.StableAndPanicConcurrency(metricKey, now)
		a.reporter.ReportStableRequestConcurrency(observedStableValue)
		a.reporter.ReportPanicRequestConcurrency(observedPanicValue)
		a.reporter.ReportTargetRequestConcurrency(spec.TargetValue)
	}

	// Put the scaling metric to logs.
	logger = logger.With(zap.String("metric", metricName))

	if err != nil {
		if err == ErrNoData {
			logger.Debug("No data to scale on yet")
		} else {
			logger.Errorw("Failed to obtain metrics", zap.Error(err))
		}
		return 0, 0, false
	}

	// Make sure we don't get stuck with the same number of pods, if the scale up rate
	// is too conservative and MaxScaleUp*RPC==RPC, so this permits us to grow at least by a single
	// pod if we need to scale up.
	// E.g. MSUR=1.1, OCC=3, RPC=2, TV=1 => OCC/TV=3, MSU=2.2 => DSPC=2, while we definitely, need
	// 3 pods. See the unit test for this scenario in action.
	maxScaleUp := math.Ceil(spec.MaxScaleUpRate * readyPodsCount)
	// Same logic, opposite math applies here.
	maxScaleDown := math.Floor(readyPodsCount / spec.MaxScaleDownRate)

	dspc := math.Ceil(observedStableValue / spec.TargetValue)
	dppc := math.Ceil(observedPanicValue / spec.TargetValue)
	logger.Debugf("DesiredStablePodCount = %0.3f, DesiredPanicPodCount = %0.3f, MaxScaleUp = %0.3f, MaxScaleDown = %0.3f",
		dspc, dppc, maxScaleUp, maxScaleDown)

	// We want to keep desired pod count in the  [maxScaleDown, maxScaleUp] range.
	desiredStablePodCount := int32(math.Min(math.Max(dspc, maxScaleDown), maxScaleUp))
	desiredPanicPodCount := int32(math.Min(math.Max(dppc, maxScaleDown), maxScaleUp))

	logger.With(zap.String("mode", "stable")).Debugf("Observed average scaling metric value: %0.3f, targeting %0.3f.",
		observedStableValue, spec.TargetValue)
	logger.With(zap.String("mode", "panic")).Debugf("Observed average scaling metric value: %0.3f, targeting %0.3f.",
		observedPanicValue, spec.TargetValue)

	isOverPanicThreshold := observedPanicValue/readyPodsCount >= spec.PanicThreshold

	a.stateMux.Lock()
	defer a.stateMux.Unlock()
	if a.panicTime.IsZero() && isOverPanicThreshold {
		// Begin panicking when we cross the threshold in the panic window.
		logger.Info("PANICKING")
		a.panicTime = now
		a.reporter.ReportPanic(1)
	} else if !a.panicTime.IsZero() && !isOverPanicThreshold && a.panicTime.Add(spec.StableWindow).Before(now) {
		// Stop panicking after the surge has made its way into the stable metric.
		logger.Info("Un-panicking.")
		a.panicTime = time.Time{}
		a.maxPanicPods = 0
		a.reporter.ReportPanic(0)
	}

	if !a.panicTime.IsZero() {
		logger.Debug("Operating in panic mode.")
		// We do not scale down while in panic mode. Only increases will be applied.
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %d to %d.", originalReadyPodsCount, desiredPanicPodCount)
			a.panicTime = now
			a.maxPanicPods = desiredPanicPodCount
		} else if desiredPanicPodCount < a.maxPanicPods {
			logger.Debugf("Skipping decrease from %d to %d.", a.maxPanicPods, desiredPanicPodCount)
		}
		desiredPodCount = a.maxPanicPods
	} else {
		logger.Debug("Operating in stable mode.")
		desiredPodCount = desiredStablePodCount
	}

	// Compute the excess burst capacity based on stable value for now, since we don't want to
	// be making knee-jerk decisions about Activator in the request path. Negative EBC means
	// that the deployment does not have enough capacity to serve the desired burst off hand.
	// EBC = TotCapacity - Cur#ReqInFlight - TargetBurstCapacity
	excessBC = int32(-1)
	switch {
	case a.deciderSpec.TargetBurstCapacity == 0:
		excessBC = 0
	case a.deciderSpec.TargetBurstCapacity >= 0:
		excessBC = int32(math.Floor(float64(originalReadyPodsCount)*a.deciderSpec.TotalValue - observedStableValue -
			a.deciderSpec.TargetBurstCapacity))
		logger.Infof("PodCount=%v Total1PodCapacity=%v ObservedStableValue=%v TargetBC=%v ExcessBC=%v",
			originalReadyPodsCount,
			a.deciderSpec.TotalValue,
			observedStableValue, a.deciderSpec.TargetBurstCapacity, excessBC)
	}

	a.reporter.ReportExcessBurstCapacity(float64(excessBC))
	a.reporter.ReportDesiredPodCount(int64(desiredPodCount))

	return desiredPodCount, excessBC, true
}

func (a *Autoscaler) currentSpecAndPC() (*DeciderSpec, resources.ReadyPodCounter) {
	a.specMux.RLock()
	defer a.specMux.RUnlock()
	return a.deciderSpec, a.podCounter
}
