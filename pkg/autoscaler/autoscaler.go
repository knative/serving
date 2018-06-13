/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"time"

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"github.com/knative/serving/pkg/logging"
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
}

type statKey struct {
	podName string
	time    time.Time
}

// holds an aggregation across all pods
type totalAggregation struct {
	perPodAggregations map[string]*perPodAggregation
	probeCount         int32
}

// aggregates a given stat to the correct pod-aggregation
func (agg *totalAggregation) aggregate(stat Stat) {
	if agg.perPodAggregations == nil {
		agg.perPodAggregations = make(map[string]*perPodAggregation)
	}
	current, exists := agg.perPodAggregations[stat.PodName]
	if !exists {
		current = &perPodAggregation{}
		agg.perPodAggregations[stat.PodName] = current
	}
	current.aggregate(stat.AverageConcurrentRequests)
	agg.probeCount += 1
}

// the number of pods that are observable via stats
func (agg *totalAggregation) observedPods() int {
	return len(agg.perPodAggregations)
}

// the observed concurrency per pod (sum of all average concurrencies
// distributed over the observed pods)
func (agg *totalAggregation) observedConcurrencyPerPod() float64 {
	accumulatedConcurrency := float64(0)
	for _, perPod := range agg.perPodAggregations {
		accumulatedConcurrency += perPod.calculateAverage()
	}
	return accumulatedConcurrency / float64(agg.observedPods())
}

// hols an aggregation per pod
type perPodAggregation struct {
	accumulatedConcurrency float64
	probeCount             int32
}

// aggregates the given concurrency
func (agg *perPodAggregation) aggregate(concurrency float64) {
	agg.accumulatedConcurrency += concurrency
	agg.probeCount += 1
}

// calculates the average concurrency over all values given
func (agg *perPodAggregation) calculateAverage() float64 {
	return agg.accumulatedConcurrency / float64(agg.probeCount)
}

var (
	lastRequestTime = time.Now()
)

// Config defines the tunable autoscaler parameters
type Config struct {
	TargetConcurrency    *k8sflag.Float64Flag
	MaxScaleUpRate       *k8sflag.Float64Flag
	StableWindow         *k8sflag.DurationFlag
	PanicWindow          *k8sflag.DurationFlag
	ScaleToZeroThreshold *k8sflag.DurationFlag
}

// Autoscaler stores current state of an instance of an autoscaler
type Autoscaler struct {
	Config
	stats        map[statKey]Stat
	panicking    bool
	panicTime    *time.Time
	maxPanicPods float64
	reporter     StatsReporter
}

// NewAutoscaler creates a new instance of autoscaler
func NewAutoscaler(config Config, reporter StatsReporter) *Autoscaler {
	return &Autoscaler{
		Config:   config,
		stats:    make(map[statKey]Stat),
		reporter: reporter,
	}
}

// Record a data point. No safe for concurrent access or concurrent access with Scale.
func (a *Autoscaler) Record(ctx context.Context, stat Stat) {
	if stat.Time == nil {
		logger := logging.FromContext(ctx)
		logger.Errorf("Missing time from stat: %+v", stat)
		return
	}
	key := statKey{
		podName: stat.PodName,
		time:    *stat.Time,
	}
	a.stats[key] = stat
}

// Scale calculates the desired scale based on current statistics given the current time.
// Not safe for concurrent access or concurrent access with Record.
func (a *Autoscaler) Scale(ctx context.Context, now time.Time) (int32, bool) {
	logger := logging.FromContext(ctx)
	// 60 second window
	stableData := &totalAggregation{}

	// 6 second window
	panicData := &totalAggregation{}

	// Last stat per Pod
	lastStat := make(map[string]Stat)

	for key, stat := range a.stats {
		instant := key.time
		if instant.Add(*a.PanicWindow.Get()).After(now) {
			panicData.aggregate(stat)
		}
		if instant.Add(*a.StableWindow.Get()).After(now) {
			stableData.aggregate(stat)

			// if there's no last stat for this pod, set it
			if _, ok := lastStat[stat.PodName]; !ok {
				lastStat[stat.PodName] = stat
			}
			// if the current last stat is older than the new one, override
			if lastStat[stat.PodName].Time.Before(*stat.Time) {
				lastStat[stat.PodName] = stat
			}
			// update lastRequestTime if the current stat is newer and
			// actually contains requests
			if lastRequestTime.Before(*stat.Time) && stat.RequestCount > 0 {
				lastRequestTime = *stat.Time
			}
		} else {
			// Drop metrics after 60 seconds
			delete(a.stats, key)
		}
	}

	// Scale to zero if the last request is from too long ago
	if lastRequestTime.Add(*a.ScaleToZeroThreshold.Get()).Before(now) {
		return 0, true
	}

	// Do nothing when we have no data.
	if stableData.observedPods() == 0 {
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

	observedStableConcurrency := stableData.observedConcurrencyPerPod()
	observedPanicConcurrency := panicData.observedConcurrencyPerPod()
	// Desired scaling ratio is observed concurrency over desired (stable) concurrency.
	// Rate limited to within MaxScaleUpRate.
	desiredStableScalingRatio := a.rateLimited(observedStableConcurrency / a.TargetConcurrency.Get())
	desiredPanicScalingRatio := a.rateLimited(observedPanicConcurrency / a.TargetConcurrency.Get())

	desiredStablePodCount := desiredStableScalingRatio * float64(stableData.observedPods())
	desiredPanicPodCount := desiredPanicScalingRatio * float64(stableData.observedPods())

	a.reporter.Report(ObservedPodCountM, float64(stableData.observedPods()))
	a.reporter.Report(ObservedStableConcurrencyM, observedStableConcurrency)
	a.reporter.Report(ObservedPanicConcurrencyM, observedPanicConcurrency)
	a.reporter.Report(TargetConcurrencyM, a.TargetConcurrency.Get())

	logger.Debugf("STABLE: Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedStableConcurrency, a.StableWindow.Get(), stableData.probeCount, stableData.observedPods())
	logger.Debugf("PANIC: Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedPanicConcurrency, a.PanicWindow.Get(), panicData.probeCount, panicData.observedPods())

	// Stop panicking after the surge has made its way into the stable metric.
	if a.panicking && a.panicTime.Add(*a.StableWindow.Get()).Before(now) {
		logger.Info("Un-panicking.")
		a.reporter.Report(PanicM, 0)
		a.panicking = false
		a.panicTime = nil
		a.maxPanicPods = 0
	}

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && panicData.observedPods() > 0 && observedPanicConcurrency >= (a.TargetConcurrency.Get()*2) {
		logger.Info("PANICKING")
		a.reporter.Report(PanicM, 1)
		a.panicking = true
		a.panicTime = &now
	}

	if a.panicking {
		logger.Debug("Operating in panic mode.")
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", panicData.observedPods(), int(desiredPanicPodCount))
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		return int32(math.Max(1.0, math.Ceil(a.maxPanicPods))), true
	}
	logger.Debug("Operating in stable mode.")
	return int32(math.Max(1.0, math.Ceil(desiredStablePodCount))), true
}

func (a *Autoscaler) rateLimited(desiredRate float64) float64 {
	if desiredRate > a.MaxScaleUpRate.Get() {
		return a.MaxScaleUpRate.Get()
	}
	return desiredRate
}
