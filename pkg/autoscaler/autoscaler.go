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
	"math"
	"strings"
	"sync"
	"time"

	"github.com/knative/pkg/logging"
	"go.uber.org/zap"
)

const (
	// ActivatorPodName defines the pod name of the activator
	// as defined in the metrics it sends.
	ActivatorPodName string = "activator"
)

// Stat defines a single measurement at a point in time
type Stat struct {
	// The time the data point was collected on the pod.
	Time *time.Time

	// The unique identity of this pod.  Used to count how many pods
	// are contributing to the metrics.
	PodName string

	// Average number of requests currently being handled by this pod.
	AverageConcurrentRequests float64

	// Number of requests received since last Stat (approximately QPS).
	RequestCount int32

	// Lameduck indicates this Pod has received a shutdown signal.
	LameDuck bool

	// ObservedPods is the IP number of a revision from k8s API at the time when
	// this stat is collected. It is the same with the pods number.
	ObservedPods int32
}

// StatMessage wraps a Stat with identifying information so it can be routed
// to the correct receiver.
type StatMessage struct {
	Key  string
	Stat Stat
}

type statKey struct {
	podName string
	time    time.Time
}

// Creates a new totalAggregation
func newTotalAggregation(window time.Duration) *totalAggregation {
	return &totalAggregation{
		window:                          window,
		perPodAggregations:              make(map[string]*perPodAggregation),
		activatorsContained:             make(map[string]struct{}),
		accumulatedConcurrency:          float64(0),
		accumulatedActivatorConcurrency: float64(0),
		accumulatedQPS:                  int32(0),
		accumulatedActivatorQPS:         int32(0),
		accumulatedObservedPods:         int32(0),
		nonLameDuckCount:                int32(0),
		sampleCount:                     int32(0),
	}
}

// Holds an aggregation across all pods
type totalAggregation struct {
	window              time.Duration
	perPodAggregations  map[string]*perPodAggregation
	probeCount          int32
	activatorsContained map[string]struct{}

	accumulatedConcurrency          float64
	accumulatedActivatorConcurrency float64
	accumulatedQPS                  int32
	accumulatedActivatorQPS         int32
	accumulatedObservedPods         int32
	activatorProbeCount             int32
	nonLameDuckCount                int32
	sampleCount                     int32
}

// Aggregates a given stat to the correct pod-aggregation
func (agg *totalAggregation) aggregate(stat Stat) {
	current, exists := agg.perPodAggregations[stat.PodName]
	if !exists {
		current = &perPodAggregation{window: agg.window}
		agg.perPodAggregations[stat.PodName] = current
	}
	if stat.LameDuck {
		current.lameduck(stat.Time)
	} else {
		current.aggregate(stat.AverageConcurrentRequests)
		if isFromActivator(stat.PodName) {
			agg.activatorsContained[stat.PodName] = struct{}{}
		}
		agg.probeCount++
	}
}

// Aggregates a given stat to the correct pod-aggregation
func (agg *totalAggregation) aggregateSample(stat Stat) {
	if isFromActivator(stat.PodName) {
		agg.activatorProbeCount++
		agg.activatorsContained[stat.PodName] = struct{}{}
		agg.accumulatedActivatorConcurrency += stat.AverageConcurrentRequests
		agg.accumulatedActivatorQPS += stat.RequestCount
	} else {
		agg.sampleCount++
		if !stat.LameDuck {
			agg.nonLameDuckCount++
			agg.accumulatedConcurrency += stat.AverageConcurrentRequests
			agg.accumulatedQPS += stat.RequestCount
			agg.accumulatedObservedPods += stat.ObservedPods
		}
	}
}

// The number of pods that are observable via stats
// Subtracts the activator pod if its not the only pod reporting stats
func (agg *totalAggregation) observedPods(now time.Time) float64 {
	podCount := float64(0.0)
	for _, pod := range agg.perPodAggregations {
		podCount += pod.usageRatio(now)
	}

	activatorsCount := len(agg.activatorsContained)
	// Discount the activators in the pod count.
	if activatorsCount > 0 {
		discountedPodCount := podCount - float64(activatorsCount)
		// Report a minimum of 1 pod if the activators are sending metrics.
		if discountedPodCount < 1.0 {
			return 1.0
		}
		return discountedPodCount
	}
	return podCount
}

// The number of available pods that are estimated via sample stats.
func (agg *totalAggregation) estimatedPods(now time.Time) float64 {
	podCount := float64(agg.accumulatedObservedPods) / float64(agg.sampleCount)
	// Report a minimum of 1 pod if the activators are sending metrics.
	if agg.activatorProbeCount > 0 && podCount < 1.0 {
		return 1.0
	}
	return podCount
}

// The observed concurrency per pod (sum of all average concurrencies
// distributed over the observed pods)
// Ignores activator sent metrics if its not the only pod reporting stats
func (agg *totalAggregation) observedConcurrencyPerPod(now time.Time) float64 {
	accumulatedConcurrency := float64(0)
	activatorConcurrency := float64(0)
	observedPods := agg.observedPods(now)
	for podName, perPod := range agg.perPodAggregations {
		if isFromActivator(podName) {
			activatorConcurrency += perPod.calculateAverage(now)
		} else {
			accumulatedConcurrency += perPod.calculateAverage(now)
		}
	}
	if accumulatedConcurrency == 0.0 {
		return activatorConcurrency / observedPods
	}
	return accumulatedConcurrency / observedPods
}

// The estimated concurrency per pod (average of concurrencies of all
// non-lameduck samples)
// Ignores activator sent metrics if its not the only pod reporting stats
func (agg *totalAggregation) estimatedConcurrencyPerPod(now time.Time, logger *zap.SugaredLogger) float64 {
	nonLameDuckCount := float64(agg.nonLameDuckCount)
	if nonLameDuckCount < 1.0 {
		// It could not reach this function if nonLameDuckCount is 0 since we do not
		// do scaling if there is no data
		logger.Warnf("got zero none lame duck pods count when calculating concurrency per pod, use 1 instead")
		nonLameDuckCount = 1.0
	}
	if agg.accumulatedConcurrency == 0.0 {
		averageConcurrencyPerActivator := agg.accumulatedActivatorConcurrency / float64(agg.activatorProbeCount)
		return averageConcurrencyPerActivator * float64(len(agg.activatorsContained)) / nonLameDuckCount
	}
	return agg.accumulatedConcurrency / nonLameDuckCount
}

// The estimated QPS of a revision during the scaling time window
func (agg *totalAggregation) estimatedQPS(now time.Time, logger *zap.SugaredLogger) float64 {
	nonLameDuckCount := float64(agg.nonLameDuckCount)
	if nonLameDuckCount < 1.0 {
		// It could not reach this function if nonLameDuckCount is 0 since we do not
		// do scaling if there is no data
		logger.Warnf("got zero none lame duck pods count when calculating concurrency per pod, use 1 instead")
		nonLameDuckCount = 1.0
	}
	if agg.accumulatedQPS == 0.0 {
		averageQPSPerActivator := float64(agg.accumulatedActivatorQPS) / float64(agg.activatorProbeCount)
		return averageQPSPerActivator * float64(len(agg.activatorsContained)) / nonLameDuckCount
	}
	return float64(agg.accumulatedQPS) / nonLameDuckCount
}

// Holds an aggregation per pod
type perPodAggregation struct {
	accumulatedConcurrency float64
	probeCount             int32
	window                 time.Duration
	lameduckTime           *time.Time
}

// Aggregates the given concurrency
func (agg *perPodAggregation) aggregate(concurrency float64) {
	agg.accumulatedConcurrency += concurrency
	agg.probeCount++
}

// Registers the earliest lameduck metric received.
func (agg *perPodAggregation) lameduck(t *time.Time) {
	if agg.lameduckTime == nil {
		agg.lameduckTime = t
	}
	if agg.lameduckTime.After(*t) {
		agg.lameduckTime = t
	}
}

// Calculates the average concurrency over all values given
func (agg *perPodAggregation) calculateAverage(now time.Time) float64 {
	if agg.probeCount == 0 {
		return 0.0
	}
	return agg.accumulatedConcurrency / float64(agg.probeCount) * agg.usageRatio(now)
}

// Calculates the weighted pod count
func (agg *perPodAggregation) usageRatio(now time.Time) float64 {
	if agg.lameduckTime == nil {
		return float64(1.0)
	}
	outOfService := now.Sub(*agg.lameduckTime)
	return float64(1.0) - (float64(outOfService) / float64(agg.window))
}

// Autoscaler stores current state of an instance of an autoscaler
type Autoscaler struct {
	*DynamicConfig
	key          string
	target       float64
	stats        map[statKey]Stat
	statsMutex   sync.Mutex
	panicking    bool
	panicTime    *time.Time
	maxPanicPods float64
	reporter     StatsReporter
	targetMutex  sync.RWMutex
}

// New creates a new instance of autoscaler
func New(dynamicConfig *DynamicConfig, target float64, reporter StatsReporter) *Autoscaler {
	return &Autoscaler{
		DynamicConfig: dynamicConfig,
		target:        target,
		stats:         make(map[statKey]Stat),
		reporter:      reporter,
	}
}

// Update reconfigures the UniScaler according to the MetricSpec.
func (a *Autoscaler) Update(spec MetricSpec) error {
	a.targetMutex.Lock()
	defer a.targetMutex.Unlock()
	a.target = spec.TargetConcurrency
	return nil
}

// Record a data point.
func (a *Autoscaler) Record(ctx context.Context, stat Stat) {
	if stat.Time == nil {
		logger := logging.FromContext(ctx)
		logger.Errorf("Missing time from stat: %+v", stat)
		return
	}
	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	key := statKey{
		podName: stat.PodName,
		time:    *stat.Time,
	}
	a.stats[key] = stat
}

// Scale calculates the desired scale based on current statistics given the current time.
func (a *Autoscaler) Scale(ctx context.Context, now time.Time) (int32, bool) {
	logger := logging.FromContext(ctx)

	a.targetMutex.RLock()
	defer a.targetMutex.RUnlock()

	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	if desiredPodCountBasedOnSample, scaled := a.scaleBasedOnSamples(ctx, now); scaled {
		logger.Infof("desiredPodCountBasedOnSample = %v", desiredPodCountBasedOnSample)
	}

	config := a.Current()

	// 60 second window
	stableData := newTotalAggregation(config.StableWindow)

	// 6 second window
	panicData := newTotalAggregation(config.PanicWindow)

	// Last stat per Pod
	lastStat := make(map[string]Stat)

	// accumulate stats into their respective buckets
	for key, stat := range a.stats {
		if stat.ObservedPods != 0.0 {
			// Skip samples
			continue
		}
		instant := key.time
		if instant.Add(config.PanicWindow).After(now) {
			panicData.aggregate(stat)
		}
		if instant.Add(config.StableWindow).After(now) {
			stableData.aggregate(stat)

			// If there's no last stat for this pod, set it
			if _, ok := lastStat[stat.PodName]; !ok {
				lastStat[stat.PodName] = stat
			}
			// If the current last stat is older than the new one, override
			if lastStat[stat.PodName].Time.Before(*stat.Time) {
				lastStat[stat.PodName] = stat
			}
		} else {
			// Drop metrics after 60 seconds
			delete(a.stats, key)
		}
	}

	// Do nothing when we have no data.
	if stableData.observedPods(now) < 1.0 {
		logger.Debug("No data to scale on.")
		return 0, false
	}

	// Log system totals
	totalCurrentQPS := int32(0)
	totalCurrentConcurrency := float64(0)
	for _, stat := range lastStat {
		totalCurrentQPS = totalCurrentQPS + stat.RequestCount
		totalCurrentConcurrency = totalCurrentConcurrency + stat.AverageConcurrentRequests
	}
	logger.Debugf("Current QPS: %v  Current concurrent clients: %v", totalCurrentQPS, totalCurrentConcurrency)

	observedStableConcurrencyPerPod := stableData.observedConcurrencyPerPod(now)
	observedPanicConcurrencyPerPod := panicData.observedConcurrencyPerPod(now)
	observedStablePodCount := stableData.observedPods(now)
	observedPanicPodCount := panicData.observedPods(now)
	// Desired scaling ratio is observed concurrency over desired (stable) concurrency.
	// Rate limited to within MaxScaleUpRate.
	desiredStableScalingRatio := a.rateLimited(observedStableConcurrencyPerPod / a.target)
	desiredPanicScalingRatio := a.rateLimited(observedPanicConcurrencyPerPod / a.target)

	desiredStablePodCount := desiredStableScalingRatio * observedStablePodCount
	desiredPanicPodCount := desiredPanicScalingRatio * observedPanicPodCount

	a.reporter.Report(ObservedPodCountM, float64(observedStablePodCount))
	a.reporter.Report(StableRequestConcurrencyM, observedStableConcurrencyPerPod)
	a.reporter.Report(PanicRequestConcurrencyM, observedPanicConcurrencyPerPod)
	a.reporter.Report(TargetConcurrencyM, a.target)

	logger.Debugf("STABLE: Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedStableConcurrencyPerPod, config.StableWindow, stableData.probeCount, observedStablePodCount)
	logger.Debugf("PANIC: Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedPanicConcurrencyPerPod, config.PanicWindow, panicData.probeCount, observedPanicPodCount)

	// Stop panicking after the surge has made its way into the stable metric.
	if a.panicking && a.panicTime.Add(config.StableWindow).Before(now) {
		logger.Info("Un-panicking.")
		a.reporter.Report(PanicM, 0)
		a.panicking = false
		a.panicTime = nil
		a.maxPanicPods = 0
	}

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && observedPanicPodCount > 0.0 && observedPanicConcurrencyPerPod >= (a.target*2) {
		logger.Info("PANICKING")
		a.reporter.Report(PanicM, 1)
		a.panicking = true
		a.panicTime = &now
	}

	var desiredPodCount int32

	if a.panicking {
		logger.Debug("Operating in panic mode.")
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", observedPanicPodCount, int(desiredPanicPodCount))
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		desiredPodCount = int32(math.Ceil(a.maxPanicPods))
	} else {
		logger.Debug("Operating in stable mode.")
		desiredPodCount = int32(math.Ceil(desiredStablePodCount))
	}

	a.reporter.Report(DesiredPodCountM, float64(desiredPodCount))
	return desiredPodCount, true
}

func (a *Autoscaler) scaleBasedOnSamples(ctx context.Context, now time.Time) (int32, bool) {
	logger := logging.FromContext(ctx)

	config := a.Current()

	// 60 second window
	stableData := newTotalAggregation(config.StableWindow)

	// 6 second window
	panicData := newTotalAggregation(config.PanicWindow)

	// accumulate stats into their respective buckets
	for key, stat := range a.stats {
		if stat.ObservedPods == 0.0 && !isFromActivator(stat.PodName) {
			continue
		}
		instant := key.time
		if instant.Add(config.PanicWindow).After(now) {
			panicData.aggregateSample(stat)
		}
		if instant.Add(config.StableWindow).After(now) {
			stableData.aggregateSample(stat)
		} else {
			// Drop metrics after 60 seconds
			delete(a.stats, key)
		}
	}

	// Do nothing when we have no data.
	if stableData.estimatedPods(now) < 1.0 {
		logger.Debug("No samples data to scale on.")
		return 0, false
	}

	stableEstimatedQPS := stableData.estimatedQPS(now, logger)
	panicEstimatedQPS := stableData.estimatedQPS(now, logger)
	logger.Debugf("Stable estimated QPS: %v  Panic estimated QPS", stableEstimatedQPS, panicEstimatedQPS)

	estimatedStableConcurrencyPerPod := stableData.estimatedConcurrencyPerPod(now, logger)
	estimatedPanicConcurrencyPerPod := panicData.estimatedConcurrencyPerPod(now, logger)
	estimatedStablePodCount := stableData.estimatedPods(now)
	estimatedPanicPodCount := stableData.estimatedPods(now)
	// Desired scaling ratio is observed concurrency over desired (stable) concurrency.
	// Rate limited to within MaxScaleUpRate.
	desiredStableScalingRatio := a.rateLimited(estimatedStableConcurrencyPerPod / a.target)
	desiredPanicScalingRatio := a.rateLimited(estimatedPanicConcurrencyPerPod / a.target)

	desiredStablePodCount := desiredStableScalingRatio * estimatedStablePodCount
	desiredPanicPodCount := desiredPanicScalingRatio * estimatedPanicPodCount

	// a.reporter.Report(ObservedPodCountM, float64(stableData.observedPods(now)))
	// a.reporter.Report(StableRequestConcurrencyM, observedStableConcurrencyPerPod)
	// a.reporter.Report(PanicRequestConcurrencyM, observedPanicConcurrencyPerPod)
	// a.reporter.Report(TargetConcurrencyM, a.target)

	logger.Debugf("STABLE: Estimated average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		estimatedStableConcurrencyPerPod, config.StableWindow, stableData.sampleCount, estimatedStablePodCount)
	logger.Debugf("PANIC: Estimated average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		estimatedPanicConcurrencyPerPod, config.PanicWindow, panicData.sampleCount, estimatedPanicPodCount)

	panicking := panicData.observedPods(now) > 0.0 && estimatedPanicConcurrencyPerPod >= (a.target*2)
	// // Stop panicking after the surge has made its way into the stable metric.
	// if a.panicking && a.panicTime.Add(config.StableWindow).Before(now) {
	// 	logger.Info("Un-panicking.")
	// 	a.reporter.Report(PanicM, 0)
	// 	a.panicking = false
	// 	a.panicTime = nil
	// 	a.maxPanicPods = 0
	// }

	// // Begin panicking when we cross the 6 second concurrency threshold.
	// if !a.panicking && panicData.observedPods(now) > 0.0 && estimatedPanicConcurrencyPerPod >= (a.target*2) {
	// 	logger.Info("PANICKING")
	// 	// a.reporter.Report(PanicM, 1)
	// 	// a.panicking = true
	// 	// a.panicTime = &now
	// }

	var desiredPodCount int32

	if panicking {
		logger.Debug("Operating in panic mode.")
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", panicData.observedPods(now), int(desiredPanicPodCount))
			// a.panicTime = &now
			// a.maxPanicPods = desiredPanicPodCount
		}
		desiredPodCount = int32(math.Ceil(a.maxPanicPods))
	} else {
		logger.Debug("Operating in stable mode.")
		desiredPodCount = int32(math.Ceil(desiredStablePodCount))
	}

	// a.reporter.Report(DesiredPodCountM, float64(desiredPodCount))
	return desiredPodCount, true
}

func (a *Autoscaler) rateLimited(desiredRate float64) float64 {
	if desiredRate > a.Current().MaxScaleUpRate {
		return a.Current().MaxScaleUpRate
	}
	return desiredRate
}

func isFromActivator(podName string) bool {
	// TODO(#2282): This can cause naming collisions.
	return strings.HasPrefix(podName, ActivatorPodName)
}
