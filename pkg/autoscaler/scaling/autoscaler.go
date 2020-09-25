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

package scaling

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/autoscaler/aggregation/max"
	"knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/resources"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// MinActivators is the minimum number of activators a revision will get.
const MinActivators = 2

type podCounter interface {
	ReadyCount() (int, error)
}

// autoscaler stores current state of an instance of an autoscaler.
type autoscaler struct {
	namespace    string
	revision     string
	metricClient metrics.MetricClient
	podCounter   podCounter
	reporterCtx  context.Context

	// State in panic mode.
	panicTime    time.Time
	maxPanicPods int32

	// delayWindow is used to defer scale-down decisions until a time
	// window has passed at the reduced concurrency.
	delayWindow *max.TimeWindow

	// specMux guards the current DeciderSpec.
	specMux     sync.RWMutex
	deciderSpec *DeciderSpec
}

// New creates a new instance of default autoscaler implementation.
func New(
	namespace, revision string,
	metricClient metrics.MetricClient,
	podCounter resources.EndpointsCounter,
	deciderSpec *DeciderSpec,
	reporterCtx context.Context) (UniScaler, error) {
	if podCounter == nil {
		return nil, errors.New("'podCounter' must not be nil")
	}
	if reporterCtx == nil {
		return nil, errors.New("stats reporter must not be nil")
	}

	delayer := max.NewTimeWindow(deciderSpec.ScaleDownDelay, tickInterval)

	return newAutoscaler(namespace, revision, metricClient,
		podCounter, deciderSpec, delayer, reporterCtx), nil
}

func newAutoscaler(
	namespace, revision string,
	metricClient metrics.MetricClient,
	podCounter podCounter,
	deciderSpec *DeciderSpec,
	delayWindow *max.TimeWindow,
	reporterCtx context.Context) *autoscaler {

	// We always start in the panic mode, if the deployment is scaled up over 1 pod.
	// If the scale is 0 or 1, normal Autoscaler behavior is fine.
	// When Autoscaler restarts we lose metric history, which causes us to
	// momentarily scale down, and that is not a desired behaviour.
	// Thus, we're keeping at least the current scale until we
	// accumulate enough data to make conscious decisions.
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
		pkgmetrics.Record(reporterCtx, panicM.M(1))
	} else {
		pkgmetrics.Record(reporterCtx, panicM.M(0))
	}

	return &autoscaler{
		namespace:    namespace,
		revision:     revision,
		metricClient: metricClient,
		reporterCtx:  reporterCtx,

		deciderSpec: deciderSpec,
		podCounter:  podCounter,

		delayWindow: delayWindow,

		panicTime:    pt,
		maxPanicPods: int32(curC),
	}
}

// Update reconfigures the UniScaler according to the DeciderSpec.
func (a *autoscaler) Update(deciderSpec *DeciderSpec) error {
	a.specMux.Lock()
	defer a.specMux.Unlock()

	a.deciderSpec = deciderSpec
	return nil
}

// Scale calculates the desired scale based on current statistics given the current time.
// desiredPodCount is the calculated pod count the autoscaler would like to set.
// validScale signifies whether the desiredPodCount should be applied or not.
// Scale is not thread safe in regards to panic state, but it's thread safe in
// regards to acquiring the decider spec.
func (a *autoscaler) Scale(ctx context.Context, now time.Time) ScaleResult {
	logger := logging.FromContext(ctx)

	spec := a.currentSpec()
	originalReadyPodsCount, err := a.podCounter.ReadyCount()
	// If the error is NotFound, then presume 0.
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Errorw("Failed to get ready pod count via K8S Lister", zap.Error(err))
		return invalidSR
	}
	// Use 1 if there are zero current pods.
	readyPodsCount := math.Max(1, float64(originalReadyPodsCount))

	metricKey := types.NamespacedName{Namespace: a.namespace, Name: a.revision}

	metricName := spec.ScalingMetric
	var observedStableValue, observedPanicValue float64
	switch spec.ScalingMetric {
	case autoscaling.RPS:
		observedStableValue, observedPanicValue, err = a.metricClient.StableAndPanicRPS(metricKey, now)
		pkgmetrics.RecordBatch(a.reporterCtx, stableRPSM.M(observedStableValue), panicRPSM.M(observedStableValue),
			targetRPSM.M(spec.TargetValue))
	default:
		metricName = autoscaling.Concurrency // concurrency is used by default
		observedStableValue, observedPanicValue, err = a.metricClient.StableAndPanicConcurrency(metricKey, now)
		pkgmetrics.RecordBatch(a.reporterCtx, stableRequestConcurrencyM.M(observedStableValue),
			panicRequestConcurrencyM.M(observedPanicValue), targetRequestConcurrencyM.M(spec.TargetValue))
	}

	// Put the scaling metric to logs.
	logger = logger.With(zap.String("metric", metricName))

	if err != nil {
		if err == metrics.ErrNoData {
			logger.Debug("No data to scale on yet")
		} else {
			logger.Errorw("Failed to obtain metrics", zap.Error(err))
		}
		return invalidSR
	}

	// Make sure we don't get stuck with the same number of pods, if the scale up rate
	// is too conservative and MaxScaleUp*RPC==RPC, so this permits us to grow at least by a single
	// pod if we need to scale up.
	// E.g. MSUR=1.1, OCC=3, RPC=2, TV=1 => OCC/TV=3, MSU=2.2 => DSPC=2, while we definitely, need
	// 3 pods. See the unit test for this scenario in action.
	maxScaleUp := math.Ceil(spec.MaxScaleUpRate * readyPodsCount)
	// Same logic, opposite math applies here.
	maxScaleDown := 0.
	if spec.Reachable {
		maxScaleDown = math.Floor(readyPodsCount / spec.MaxScaleDownRate)
	}

	dspc := math.Ceil(observedStableValue / spec.TargetValue)
	dppc := math.Ceil(observedPanicValue / spec.TargetValue)
	logger.Debugf("DesiredStablePodCount = %0.3f, DesiredPanicPodCount = %0.3f, ReadyEndpointCount = %d, MaxScaleUp = %0.3f, MaxScaleDown = %0.3f",
		dspc, dppc, originalReadyPodsCount, maxScaleUp, maxScaleDown)

	// We want to keep desired pod count in the  [maxScaleDown, maxScaleUp] range.
	desiredStablePodCount := int32(math.Min(math.Max(dspc, maxScaleDown), maxScaleUp))
	desiredPanicPodCount := int32(math.Min(math.Max(dppc, maxScaleDown), maxScaleUp))

	logger.With(zap.String("mode", "stable")).Debugf("Observed average scaling metric value: %0.3f, targeting %0.3f.",
		observedStableValue, spec.TargetValue)
	logger.With(zap.String("mode", "panic")).Debugf("Observed average scaling metric value: %0.3f, targeting %0.3f.",
		observedPanicValue, spec.TargetValue)

	isOverPanicThreshold := dppc/readyPodsCount >= spec.PanicThreshold

	if a.panicTime.IsZero() && isOverPanicThreshold {
		// Begin panicking when we cross the threshold in the panic window.
		logger.Info("PANICKING.")
		a.panicTime = now
		pkgmetrics.Record(a.reporterCtx, panicM.M(1))
	} else if isOverPanicThreshold {
		// If we're still over panic threshold right now — extend the panic window.
		a.panicTime = now
	} else if !a.panicTime.IsZero() && !isOverPanicThreshold && a.panicTime.Add(spec.StableWindow).Before(now) {
		// Stop panicking after the surge has made its way into the stable metric.
		logger.Info("Un-panicking.")
		a.panicTime = time.Time{}
		a.maxPanicPods = 0
		pkgmetrics.Record(a.reporterCtx, panicM.M(0))
	}

	desiredPodCount := desiredStablePodCount
	if !a.panicTime.IsZero() {
		// In some edgecases stable window metric might be larger
		// than panic one. And we should provision for stable as for panic,
		// so pick the larger of the two.
		if desiredPodCount < desiredPanicPodCount {
			desiredPodCount = desiredPanicPodCount
		}
		logger.Debug("Operating in panic mode.")
		// We do not scale down while in panic mode. Only increases will be applied.
		if desiredPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %d to %d.", originalReadyPodsCount, desiredPodCount)
			a.maxPanicPods = desiredPodCount
		} else if desiredPodCount < a.maxPanicPods {
			logger.Infof("Skipping decrease from %d to %d.", a.maxPanicPods, desiredPodCount)
		}
		desiredPodCount = a.maxPanicPods
	} else {
		logger.Debug("Operating in stable mode.")
	}

	// Delay scale down decisions, if a ScaleDownDelay was specified.
	// We only do this if there's a ScaleDownDelay because although a one-element
	// delay window is _almost_ the same as no delay at all, it is not the same
	// in the case where two Scale()s happen in the same time interval (because
	// the largest will be picked rather than the most recent in that case).
	if a.deciderSpec.ScaleDownDelay > 0 {
		a.delayWindow.Record(now, desiredPodCount)
		delayedPodCount := a.delayWindow.Current()
		if delayedPodCount != desiredPodCount {
			logger.Debugf("Delaying scale to %d, staying at %d", desiredPodCount, delayedPodCount)
			desiredPodCount = delayedPodCount
		}
	}

	// Here we compute two numbers: excess burst capacity and number of activators
	// for subsetting.
	// - the excess burst capacity is based on panic value, since we don't want to
	//   be making knee-jerk decisions about Activator in the request path.
	//   Negative EBC means that the deployment does not have enough capacity to serve
	//   the desired burst off hand.
	//   EBC = TotCapacity - Cur#ReqInFlight - TargetBurstCapacity
	// - number of activators is based on total capacity and TargetBurstCapacity values.
	//   if tbc==0, then activators are in play only for scale from 0 and the revision gets
	//   the default number.
	//   if tbc > 0, then revision gets number of activators to support total capacity and
	//   tbc additional units.
	//   if tbc==-1, then revision gets the number of activators needed to support total capacity.
	//   With default target utilization of 0.7, we're overprovisioning number of needed activators
	//   by rate of 1/0.7=1.42.
	excessBCF := -1.
	numAct := int32(MinActivators)
	switch {
	case a.deciderSpec.TargetBurstCapacity == 0:
		excessBCF = 0
		// numAct stays at MinActivators, only needed to scale from 0.
	case a.deciderSpec.TargetBurstCapacity > 0:
		totCap := float64(originalReadyPodsCount) * a.deciderSpec.TotalValue
		excessBCF = math.Floor(totCap - a.deciderSpec.TargetBurstCapacity - observedPanicValue)
		numAct = int32(math.Max(MinActivators,
			math.Ceil((totCap+a.deciderSpec.TargetBurstCapacity)/a.deciderSpec.ActivatorCapacity)))
	case a.deciderSpec.TargetBurstCapacity == -1:
		numAct = int32(math.Max(MinActivators,
			math.Ceil(float64(originalReadyPodsCount)*a.deciderSpec.TotalValue/a.deciderSpec.ActivatorCapacity)))
	}
	logger.Debugf("PodCount=%d Total1PodCapacity=%0.3f ObsStableValue=%0.3f ObsPanicValue=%0.3f TargetBC=%0.3f ExcessBC=%0.3f NumActivators=%d",
		originalReadyPodsCount, a.deciderSpec.TotalValue, observedStableValue,
		observedPanicValue, a.deciderSpec.TargetBurstCapacity, excessBCF, numAct)

	pkgmetrics.RecordBatch(a.reporterCtx, excessBurstCapacityM.M(excessBCF),
		desiredPodCountM.M(int64(desiredPodCount)))

	return ScaleResult{
		DesiredPodCount:     desiredPodCount,
		ExcessBurstCapacity: int32(excessBCF),
		NumActivators:       numAct,
		ScaleValid:          true,
	}
}

func (a *autoscaler) currentSpec() *DeciderSpec {
	a.specMux.RLock()
	defer a.specMux.RUnlock()
	return a.deciderSpec
}
