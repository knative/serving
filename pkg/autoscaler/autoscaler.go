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

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

const (
	// KBufferPodName defines the pod name of the kbuffer
	// as defined in the metrics it sends.
	KBufferPodName string = "kbuffer"
	mockPodName    string = "mock-pod"
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
		window:             window,
		perPodAggregations: make(map[string]*perPodAggregation),
		kbuffersContained:  make(map[string]struct{}),
	}
}

// Holds an aggregation across all pods
type totalAggregation struct {
	window             time.Duration
	perPodAggregations map[string]*perPodAggregation
	probeCount         int32
	kbuffersContained  map[string]struct{}
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
		// TODO(#2282): This can cause naming collisions.
		if strings.HasPrefix(stat.PodName, KBufferPodName) {
			agg.kbuffersContained[stat.PodName] = struct{}{}
		}
		agg.probeCount++
	}
}

func (agg *totalAggregation) tickedStableWindow() bool  {
	for _, podStas := range agg.perPodAggregations {
		if podStas.accumulatedConcurrency > 0.0 {
			return false
		}
	}
	return true
}

func (agg *totalAggregation) containValuableStat() bool  {
	for podName, podStas := range agg.perPodAggregations {
		if !strings.EqualFold(podName, mockPodName) {
			if podStas.accumulatedConcurrency > 0.0 {
				return true
			}
		}
	}
	return false
}

func (agg *totalAggregation) containMockStat() bool {
	for podName, _:= range agg.perPodAggregations {
		if strings.EqualFold(podName, mockPodName) {
			return true
		}
	}
	return false
}

// The number of pods that are observable via stats
// Subtracts the kbuffer pod if its not the only pod reporting stats
func (agg *totalAggregation) observedPods(now time.Time) float64 {
	podCount := float64(0.0)
	for _, pod := range agg.perPodAggregations {
		podCount += pod.usageRatio(now)
	}

	kbuffersCount := len(agg.kbuffersContained)
	// Discount the kbuffers in the pod count.
	if kbuffersCount > 0 {
		discountedPodCount := podCount - float64(kbuffersCount)
		// Report a minimum of 1 pod if the kbuffers are sending metrics.
		return math.Max(1.0, discountedPodCount)
	}
	return podCount
}

// The observed concurrency per pod (sum of all average concurrencies
// distributed over the observed pods)
// Ignores kbuffer sent metrics if its not the only pod reporting stats
func (agg *totalAggregation) observedConcurrencyPerPod(now time.Time) float64 {
	accumulatedConcurrency := float64(0)
	kbufferConcurrency := float64(0)
	observedPods := agg.observedPods(now)
	containMock := false
	for podName, perPod := range agg.perPodAggregations {
		// TODO(#2282): This can cause naming collisions.
		if strings.HasPrefix(podName, KBufferPodName) {
			kbufferConcurrency += perPod.calculateAverage(now)
		} else {
			accumulatedConcurrency += perPod.calculateAverage(now)
		}
		if strings.HasPrefix(podName, mockPodName) {
			containMock = true
		}
	}
	if accumulatedConcurrency == 0.0 {
		return kbufferConcurrency / observedPods
	}
	// only eliminate the stat of mock pod when there contains both mock and value stat
	if containMock && accumulatedConcurrency > 1.0 {
		accumulatedConcurrency--
		observedPods--
	}
	return accumulatedConcurrency / observedPods
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
	containerConcurrency v1alpha1.RevisionContainerConcurrencyType
	stats                map[statKey]Stat
	statsMutex           sync.Mutex
	panicking            bool
	panicTime            *time.Time
	maxPanicPods         float64
	reporter             StatsReporter
	keepAliveTimes       int32
	gotStat              bool
}

// New creates a new instance of autoscaler
func New(dynamicConfig *DynamicConfig, containerConcurrency v1alpha1.RevisionContainerConcurrencyType, reporter StatsReporter) *Autoscaler {
	autoscaler := Autoscaler{
		DynamicConfig:        dynamicConfig,
		containerConcurrency: containerConcurrency,
		stats:                make(map[statKey]Stat),
		reporter:             reporter,
		keepAliveTimes:       2, // should get from config
		gotStat:              false,
	}
	autoscaler.mockStat(time.Now())
	return &autoscaler
}

// should not use mutex
func (a *Autoscaler) mockStat(now time.Time) {
	stat := Stat{
		Time:                      &now,
		PodName:                   mockPodName,
		AverageConcurrentRequests: 1,
		RequestCount:              1,
	}
	key := statKey{
		podName: stat.PodName,
		time:    *stat.Time,
	}
	a.stats[key] = stat
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
	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	config := a.Current()

	// 60 second window
	stableData := newTotalAggregation(config.StableWindow)

	// 6 second window
	panicData := newTotalAggregation(config.PanicWindow)

	// Last stat per Pod
	lastStat := make(map[string]Stat)

	// accumulate stats into their respective buckets
	for key, stat := range a.stats {
		instant := key.time

		// mark the revision is not initiating deployed
		if a.gotStat == false && !strings.EqualFold(key.podName, mockPodName) {
			a.gotStat = true
		}

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
			// Drop metrics after config.StableWindow
			delete(a.stats, key)
		}
	}

	//// Do nothing when we have no data.
	if (stableData.containMockStat() && stableData.observedPods(now) - 1.0 == 0.0) || stableData.observedPods(now) == 0.0 {
		logger.Debug("No data to scale on.")
		return 0, false
	}

	// the revision is still not ready when the first initiation
	if !a.gotStat {
		logger.Infof("lvjing2 got stat still false")
		return 1, true
	}

	if stableData.containValuableStat() {
		logger.Infof("Reset keepAliveTimes")
		a.keepAliveTimes = 2
	}
	if stableData.tickedStableWindow() && a.keepAliveTimes > 0 {
		logger.Infof("Mock the stat to start a new stable window")
		a.keepAliveTimes--
		a.mockStat(now)
		return 1, true
	}
	// return -1, so mark this revision as inactive for updating route to activator in the last stable window
	if a.keepAliveTimes == 1 {
		logger.Infof("Set the kpa to inactive")
		return -1, true
	}

	// Log system totals
	totalCurrentQPS := int32(0)
	totalCurrentConcurrency := float64(0)
	for _, stat := range lastStat {
		totalCurrentQPS = totalCurrentQPS + stat.RequestCount
		totalCurrentConcurrency = totalCurrentConcurrency + stat.AverageConcurrentRequests
	}
	logger.Debugf("Current QPS: %v  Current concurrent clients: %v", totalCurrentQPS, totalCurrentConcurrency)

	observedStableConcurrencyPerPod := float64(0.0)
	observedPanicConcurrencyPerPod := float64(0.0)
	if stableData.observedPods(now) > 0.0 {
		observedStableConcurrencyPerPod = stableData.observedConcurrencyPerPod(now)
	}
	if panicData.observedPods(now) > 0.0 {
		observedPanicConcurrencyPerPod = panicData.observedConcurrencyPerPod(now)
	}
	// Desired scaling ratio is observed concurrency over desired (stable) concurrency.
	// Rate limited to within MaxScaleUpRate.
	desiredStableScalingRatio := a.rateLimited(observedStableConcurrencyPerPod / config.TargetConcurrency(a.containerConcurrency))
	desiredPanicScalingRatio := a.rateLimited(observedPanicConcurrencyPerPod / config.TargetConcurrency(a.containerConcurrency))

	desiredStablePodCount := desiredStableScalingRatio * stableData.observedPods(now)
	desiredPanicPodCount := desiredPanicScalingRatio * stableData.observedPods(now)

	a.reporter.Report(ObservedPodCountM, float64(stableData.observedPods(now)))
	a.reporter.Report(ObservedStableConcurrencyM, observedStableConcurrencyPerPod)
	a.reporter.Report(ObservedPanicConcurrencyM, observedPanicConcurrencyPerPod)
	a.reporter.Report(TargetConcurrencyM, config.TargetConcurrency(a.containerConcurrency))

	logger.Infof("STABLE: Observed average %0.3f concurrency over %v seconds over %v samples over %v pods. desired %v",
		observedStableConcurrencyPerPod, config.StableWindow, stableData.probeCount, stableData.observedPods(now), desiredStablePodCount)
	logger.Infof("PANIC: Observed average %0.3f concurrency over %v seconds over %v samples over %v pods. desired %v",
		observedPanicConcurrencyPerPod, config.PanicWindow, panicData.probeCount, panicData.observedPods(now), desiredPanicPodCount)

	// Stop panicking after the surge has made its way into the stable metric.
	if a.panicking && a.panicTime.Add(config.StableWindow).Before(now) {
		logger.Info("Un-panicking.")
		a.reporter.Report(PanicM, 0)
		a.panicking = false
		a.panicTime = nil
		a.maxPanicPods = 0
	}

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && panicData.observedPods(now) > 0.0 && observedPanicConcurrencyPerPod >= (config.TargetConcurrency(a.containerConcurrency)*2) {
		logger.Info("PANICKING")
		a.reporter.Report(PanicM, 1)
		a.panicking = true
		a.panicTime = &now
	}

	var desiredPodCount int32

	if a.panicking {
		logger.Debug("Operating in panic mode.")
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", panicData.observedPods(now), int(desiredPanicPodCount))
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

func (a *Autoscaler) rateLimited(desiredRate float64) float64 {
	if desiredRate > a.Current().MaxScaleUpRate {
		return a.Current().MaxScaleUpRate
	}
	return desiredRate
}
