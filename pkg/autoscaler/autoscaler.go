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
	"math"
	"sync"
	"time"

	"github.com/golang/glog"
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

// StatMessage wraps a Stat with identifying information so it can be routed
// to the correct receiver.
type StatMessage struct {
	RevisionKey string
	Stat        Stat
}

type statKey struct {
	podName string
	time    time.Time
}

var (
	lastRequestTime = time.Now()
)

// Config defines the tunable autoscaler parameters
type Config struct {
	TargetConcurrency    float64
	MaxScaleUpRate       float64
	StableWindow         time.Duration
	PanicWindow          time.Duration
	ScaleToZeroThreshold time.Duration
}

// Autoscaler stores current state of an instance of an autoscaler
type Autoscaler struct {
	Config
	stats        map[statKey]Stat
	statsMutex   sync.Mutex
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

// Record a data point.
func (a *Autoscaler) Record(stat Stat) {
	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	if stat.Time == nil {
		glog.Errorf("Missing time from stat: %+v", stat)
		return
	}
	key := statKey{
		podName: stat.PodName,
		time:    *stat.Time,
	}
	a.stats[key] = stat
}

// Scale calculates the desired scale based on current statistics given the current time.
func (a *Autoscaler) Scale(now time.Time) (int32, bool) {
	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	// 60 second window
	stableTotal := float64(0)
	stableCount := float64(0)
	stablePods := make(map[string]bool)

	// 6 second window
	panicTotal := float64(0)
	panicCount := float64(0)
	panicPods := make(map[string]bool)

	// Last stat per Pod
	lastStat := make(map[string]Stat)

	for key, stat := range a.stats {
		instant := key.time
		if instant.Add(a.PanicWindow).After(now) {
			panicTotal = panicTotal + stat.AverageConcurrentRequests
			panicCount = panicCount + 1
			panicPods[stat.PodName] = true
		}
		if instant.Add(a.StableWindow).After(now) {
			stableTotal = stableTotal + stat.AverageConcurrentRequests
			stableCount = stableCount + 1
			stablePods[stat.PodName] = true

			if _, ok := lastStat[stat.PodName]; !ok {
				lastStat[stat.PodName] = stat
			}
			if lastStat[stat.PodName].Time.Before(*stat.Time) {
				lastStat[stat.PodName] = stat
			}
			if lastRequestTime.Before(*stat.Time) && stat.RequestCount > 0 {
				lastRequestTime = *stat.Time
			}
		} else {
			// Drop metrics after 60 seconds
			delete(a.stats, key)
		}
	}

	if lastRequestTime.Add(a.ScaleToZeroThreshold).Before(now) {
		glog.Info("Threshold passed with no new requests. Scaling to 0.")
		return 0, true
	}

	// Log system totals
	totalCurrentQPS := int32(0)
	totalCurrentConcurrency := float64(0)
	for _, stat := range lastStat {
		totalCurrentQPS = totalCurrentQPS + stat.RequestCount
		totalCurrentConcurrency = totalCurrentConcurrency + stat.AverageConcurrentRequests
	}
	glog.Infof("Current QPS: %v  Current concurrent clients: %v", totalCurrentQPS, totalCurrentConcurrency)

	// Stop panicking after the surge has made its way into the stable metric.
	if a.panicking && a.panicTime.Add(a.StableWindow).Before(now) {
		glog.Info("Un-panicking.")
		a.reporter.Report(PanicM, 0)
		a.panicking = false
		a.panicTime = nil
		a.maxPanicPods = 0
	}

	// Do nothing when we have no data.
	if len(stablePods) == 0 {
		glog.Info("No data to scale on.")
		return 0, false
	}

	// Observed concurrency is the average of all data points in each window
	observedStableConcurrency := stableTotal / stableCount
	observedPanicConcurrency := panicTotal / panicCount

	// Desired scaling ratio is observed concurrency over desired
	// (stable) concurrency. Rate limited to within MaxScaleUpRate.
	desiredStableScalingRatio := a.rateLimited(observedStableConcurrency / a.TargetConcurrency)
	desiredPanicScalingRatio := a.rateLimited(observedPanicConcurrency / a.TargetConcurrency)

	desiredStablePodCount := desiredStableScalingRatio * float64(len(stablePods))
	desiredPanicPodCount := desiredPanicScalingRatio * float64(len(stablePods))

	glog.Infof("Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedStableConcurrency, a.StableWindow, stableCount, len(stablePods))
	glog.Infof("Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedPanicConcurrency, a.PanicWindow, panicCount, len(panicPods))

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && len(panicPods) > 0 && observedPanicConcurrency >= (a.TargetConcurrency*2) {
		glog.Info("PANICKING")
		a.reporter.Report(PanicM, 1)
		a.panicking = true
		a.panicTime = &now
	}

	if a.panicking {
		glog.Info("Operating in panic mode.")
		if desiredPanicPodCount > a.maxPanicPods {
			glog.Infof("Increasing pods from %v to %v.", len(panicPods), int(desiredPanicPodCount))
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		return int32(math.Max(1.0, math.Ceil(a.maxPanicPods))), true
	}
	glog.Info("Operating in stable mode.")
	return int32(math.Max(1.0, math.Ceil(desiredStablePodCount))), true
}

func (a *Autoscaler) rateLimited(desiredRate float64) float64 {
	if desiredRate > a.MaxScaleUpRate {
		return a.MaxScaleUpRate
	}
	return desiredRate
}
