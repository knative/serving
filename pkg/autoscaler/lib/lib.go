package lib

import (
	"log"
	"math"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"
)

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

	stableTotal := float64(0)
	stableCount := float64(0)
	stablePods := make(map[string]bool)

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

	observedStableConcurrency := stableTotal / stableCount
	desiredStablePodCount := (observedStableConcurrency / a.stableConcurrencyPerPod) * float64(len(stablePods))

	observedPanicConcurrency := panicTotal / panicCount
	desiredPanicPodCount := (observedPanicConcurrency / a.stableConcurrencyPerPod) * float64(len(stablePods))

	log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
		observedStableConcurrency, stableWindowSeconds, stableCount, len(stablePods))
	log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
		observedPanicConcurrency, panicWindowSeconds, panicCount, len(panicPods))

	// Begin panicking when we cross the short-term QPS per pod threshold.
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
