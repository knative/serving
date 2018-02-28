package autoscaler

import (
	"math"
	"time"

	"github.com/golang/glog"
)

type Stat struct {
	// The time the data point was collected on the pod.
	Time *time.Time

	// The unique identity of this pod.  Used to count how many pods
	// are contributing to the metrics.
	PodName string

	// Number of requests currently being handled by this pod.
	ConcurrentRequests int32
}

type statKey struct {
	podName string
	time    time.Time
}

const (
	stableWindowSeconds float64       = 60
	stableWindow        time.Duration = 60 * time.Second
	panicWindowSeconds  float64       = 6
	panicWindow         time.Duration = 6 * time.Second
	maxScaleUpRate      float64       = 10
)

type Autoscaler struct {
	stableConcurrencyPerPod         float64
	panicConcurrencyPerPodThreshold float64
	stats                           map[statKey]Stat
	panicking                       bool
	panicTime                       *time.Time
	maxPanicPods                    float64
}

func NewAutoscaler(targetConcurrency float64) *Autoscaler {
	return &Autoscaler{
		stableConcurrencyPerPod:         targetConcurrency,
		panicConcurrencyPerPodThreshold: targetConcurrency * 2,
		stats: make(map[statKey]Stat),
	}
}

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

func (a *Autoscaler) Scale(now time.Time) (int32, bool) {

	// 60 second window
	stableTotal := float64(0)
	stableCount := float64(0)
	stablePods := make(map[string]bool)

	// 6 second window
	panicTotal := float64(0)
	panicCount := float64(0)
	panicPods := make(map[string]bool)

	for key, stat := range a.stats {
		instant := key.time
		if instant.Add(panicWindow).After(now) {
			panicTotal = panicTotal + float64(stat.ConcurrentRequests)
			panicCount = panicCount + 1
			panicPods[stat.PodName] = true
		}
		if instant.Add(stableWindow).After(now) {
			stableTotal = stableTotal + float64(stat.ConcurrentRequests)
			stableCount = stableCount + 1
			stablePods[stat.PodName] = true
		} else {
			// Drop metrics after 60 seconds
			delete(a.stats, key)
		}
	}

	// Stop panicking after the surge has made its way into the stable metric.
	if a.panicking && a.panicTime.Add(stableWindow).Before(now) {
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
	// (stable) concurrency. Rate limited to within maxScaleUpRate.
	desiredStableScalingRatio := rateLimited(observedStableConcurrency / a.stableConcurrencyPerPod)
	desiredPanicScalingRatio := rateLimited(observedPanicConcurrency / a.stableConcurrencyPerPod)

	desiredStablePodCount := desiredStableScalingRatio * float64(len(stablePods))
	desiredPanicPodCount := desiredPanicScalingRatio * float64(len(stablePods))

	glog.Infof("Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedStableConcurrency, stableWindowSeconds, stableCount, len(stablePods))
	glog.Infof("Observed average %0.3f concurrency over %v seconds over %v samples over %v pods.",
		observedPanicConcurrency, panicWindowSeconds, panicCount, len(panicPods))

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && len(panicPods) > 0 && observedPanicConcurrency >= a.panicConcurrencyPerPodThreshold {
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
	} else {
		glog.Info("Operating in stable mode.")
		return int32(math.Max(1.0, math.Ceil(desiredStablePodCount))), true
	}
}

func rateLimited(desiredRate float64) float64 {
	if desiredRate > maxScaleUpRate {
		return maxScaleUpRate
	}
	return desiredRate
}
