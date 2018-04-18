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
	"time"

	"github.com/golang/glog"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"
)

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

var (
	lastRequestTime = time.Now()
)

type Config struct {
	TargetConcurrency    *k8sflag.Float64Flag
	MaxScaleUpRate       *k8sflag.Float64Flag
	StableWindow         *k8sflag.DurationFlag
	PanicWindow          *k8sflag.DurationFlag
	ScaleToZeroThreshold *k8sflag.DurationFlag
}

type Autoscaler struct {
	Config
	stats        map[statKey]Stat
	panicking    bool
	panicTime    *time.Time
	maxPanicPods float64
}

func NewAutoscaler(config Config) *Autoscaler {
	return &Autoscaler{
		Config: config,
		stats:  make(map[statKey]Stat),
	}
}

// Record a data point. No safe for concurrent access or concurrent access with Scale.
func (a *Autoscaler) Record(stat Stat) {
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

// Calculate the desired scale based on current statistics given the current time.
// Not safe for concurrent access or concurrent access with Record.
func (a *Autoscaler) Scale(now time.Time) (int32, bool) {

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
		if instant.Add(*a.PanicWindow.Get()).After(now) {
			panicTotal = panicTotal + stat.AverageConcurrentRequests
			panicCount = panicCount + 1
			panicPods[stat.PodName] = true
		}
		if instant.Add(*a.StableWindow.Get()).After(now) {
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

	if lastRequestTime.Add(*a.ScaleToZeroThreshold.Get()).Before(now) {
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
	if a.panicking && a.panicTime.Add(*a.StableWindow.Get()).Before(now) {
		glog.Info("Un-panicking.")
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
	desiredStableScalingRatio := a.rateLimited(observedStableConcurrency / a.TargetConcurrency.Get())
	desiredPanicScalingRatio := a.rateLimited(observedPanicConcurrency / a.TargetConcurrency.Get())

	desiredStablePodCount := desiredStableScalingRatio * float64(len(stablePods))
	desiredPanicPodCount := desiredPanicScalingRatio * float64(len(stablePods))

	glog.Infof("Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedStableConcurrency, a.StableWindow, stableCount, len(stablePods))
	glog.Infof("Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedPanicConcurrency, a.PanicWindow, panicCount, len(panicPods))

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && len(panicPods) > 0 && observedPanicConcurrency >= (a.TargetConcurrency.Get()*2) {
		glog.Info("PANICKING")
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
	if desiredRate > a.MaxScaleUpRate.Get() {
		return a.MaxScaleUpRate.Get()
	}
	return desiredRate
}
