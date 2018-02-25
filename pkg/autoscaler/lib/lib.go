package lib

import (
	"log"
	"math"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"
)

// Autoscaler calculates the number of pods necessary for the desired
// level of concurrency per pod (stableConcurrencyPerPod). It operates in
// two modes, stable mode and panic mode.

// Stable mode calculates the average concurrency observed over the last
// 60 seconds and adjusts the observed pod count to achieve the target
// value. Current observed pod count is the number of unique pod names
// which show up in the last 60 seconds.

// Panic mode calculates the average concurrency observed over the last 6
// seconds and adjusts the observed pod count to achieve the stable
// target value. Panic mode is engaged when the observed 6 second average
// concurrency reaches 2x the target stable concurrency. Panic mode will
// last at least 60 seconds--longer if the 2x threshold is repeatedly
// breached. During panic mode the number of pods is never decreased in
// order to prevent flapping.

const (
	stableWindowSeconds float64       = 60
	stableWindow        time.Duration = 60 * time.Second
	panicWindowSeconds  float64       = 6
	panicWindow         time.Duration = 6 * time.Second
)

type Autoscaler struct {
	stableConcurrencyPerPod         float64
	panicConcurrencyPerPodThreshold float64
	stats                           map[time.Time]types.Stat
	panicking                       bool
	panicTime                       *time.Time
	maxPanicPods                    float64
}

func NewAutoscaler(targetConcurrency float64) *Autoscaler {
	return &Autoscaler{
		stableConcurrencyPerPod:         targetConcurrency,
		panicConcurrencyPerPodThreshold: targetConcurrency * 2,
		stats: make(map[time.Time]types.Stat),
	}
}

func (a *Autoscaler) Record(stat types.Stat, now time.Time) {
	a.stats[now] = stat
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

	for sec, stat := range a.stats {
		if sec.Add(panicWindow).After(now) {
			panicTotal = panicTotal + float64(stat.ConcurrentRequests)
			panicCount = panicCount + 1
			panicPods[stat.PodName] = true
		}
		if sec.Add(stableWindow).After(now) {
			stableTotal = stableTotal + float64(stat.ConcurrentRequests)
			stableCount = stableCount + 1
			stablePods[stat.PodName] = true
		} else {
			// Drop metrics after 60 seconds
			delete(a.stats, sec)
		}
	}

	// Stop panicking after the surge has made its way into the stable metric.
	if a.panicking && a.panicTime.Add(stableWindow).Before(now) {
		log.Println("Un-panicking.")
		a.panicking = false
		a.panicTime = nil
		a.maxPanicPods = 0
	}

	// Do nothing when we have no data.
	if len(stablePods) == 0 {
		log.Println("No data to scale on.")
		return 0, false
	}

	// Observed concurrency is the average of all data points in each window
	observedStableConcurrency := stableTotal / stableCount
	observedPanicConcurrency := panicTotal / panicCount

	// Desired pod count is the ratio of observed concurrency to
	// desired (stable) concurrency times the observed pod count.
	desiredStablePodCount := (observedStableConcurrency / a.stableConcurrencyPerPod) * float64(len(stablePods))
	desiredPanicPodCount := (observedPanicConcurrency / a.stableConcurrencyPerPod) * float64(len(stablePods))

	log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
		observedStableConcurrency, stableWindowSeconds, stableCount, len(stablePods))
	log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
		observedPanicConcurrency, panicWindowSeconds, panicCount, len(panicPods))

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && len(panicPods) > 0 && observedPanicConcurrency >= a.panicConcurrencyPerPodThreshold {
		log.Println("PANICKING")
		a.panicking = true
		a.panicTime = &now
	}

	if a.panicking {
		log.Printf("Operating in panic mode.")
		if desiredPanicPodCount > a.maxPanicPods {
			log.Printf("Increasing pods from %v to %v.", len(panicPods), int(desiredPanicPodCount))
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		return int32(math.Max(1.0, math.Ceil(a.maxPanicPods))), true
	} else {
		log.Println("Operating in stable mode.")
		return int32(math.Max(1.0, math.Ceil(desiredStablePodCount))), true
	}
}
