package lib

import (
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"
)

var (
	stableWindowSeconds     float64       = 60
	stableWindow            time.Duration = 60 * time.Second
	stableConcurrencyPerPod float64

	panicWindowSeconds              float64       = 6
	panicWindow                     time.Duration = 6 * time.Second
	panicConcurrencyPerPodThreshold float64
)

func init() {
	targetConcurrencyPerProcess := os.Getenv("ELA_TARGET_CONCURRENCY")
	if targetConcurrencyPerProcess == "" {
		stableConcurrencyPerPod = 10
	} else {
		concurrency, err := strconv.Atoi(targetConcurrencyPerProcess)
		if err != nil {
			panic(err)
		}
		stableConcurrencyPerPod = float64(concurrency)
	}
	panicConcurrencyPerPodThreshold = stableConcurrencyPerPod * 2
	log.Printf("Target concurrency: %0.2f. Panic threshold %0.2f", stableConcurrencyPerPod, panicConcurrencyPerPodThreshold)
}

type Autoscaler struct {
	tickerChan <-chan time.Time
	statChan   chan types.Stat
	scaleToFn  func(int32)
}

func NewAutoscaler(tickerChan <-chan time.Time, statChan chan types.Stat, scaleToFn func(int32)) *Autoscaler {
	return &Autoscaler{
		tickerChan: tickerChan,
		statChan:   statChan,
		scaleToFn:  scaleToFn,
	}
}

func (a *Autoscaler) Run() {

	// Record QPS per unique observed pod so missing data doesn't skew the results
	var stats = make(map[time.Time]types.Stat)
	var panicTime *time.Time
	var maxPanicPods float64 = 0

	record := func(stat types.Stat) {
		now := time.Now()
		stats[now] = stat
	}

	tick := func() {

		stableTotal := float64(0)
		stableCount := float64(0)
		stablePods := make(map[string]bool)

		panicTotal := float64(0)
		panicCount := float64(0)
		panicPods := make(map[string]bool)

		now := time.Now()
		for sec, stat := range stats {
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
				delete(stats, sec)
			}
		}

		// Stop panicking after the surge has made its way into the stable metric.
		if panicTime != nil && panicTime.Add(stableWindow).Before(time.Now()) {
			log.Println("Un-panicking.")
			panicTime = nil
			maxPanicPods = 0
		}

		// Do nothing when we have no data.
		if len(stablePods) == 0 {
			log.Println("No data to scale on.")
			return
		}

		observedStableConcurrency := stableTotal / stableCount
		desiredStablePodCount := (observedStableConcurrency/stableConcurrencyPerPod)*float64(len(stablePods)) + 1

		observedPanicConcurrency := panicTotal / panicCount
		desiredPanicPodCount := (observedPanicConcurrency/stableConcurrencyPerPod)*float64(len(panicPods)) + 1

		log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
			observedStableConcurrency, stableWindowSeconds, stableCount, len(stablePods))
		log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
			observedPanicConcurrency, panicWindowSeconds, panicCount, len(panicPods))

		// Begin panicking when we cross the short-term QPS per pod threshold.
		if panicTime == nil && len(panicPods) > 0 && observedPanicConcurrency > panicConcurrencyPerPodThreshold {
			log.Println("PANICKING")
			tmp := time.Now()
			panicTime = &tmp
		}

		if panicTime != nil {
			log.Printf("Operating in panic mode.")
			if desiredPanicPodCount > maxPanicPods {
				log.Printf("Continue PANICKING. Increasing pods from %v to %v.", len(panicPods), desiredPanicPodCount)
				tmp := time.Now()
				panicTime = &tmp
				maxPanicPods = desiredPanicPodCount
			}
			go a.scaleToFn(int32(math.Floor(maxPanicPods)))
		} else {
			log.Println("Operating in stable mode.")
			go a.scaleToFn(int32(math.Floor(desiredStablePodCount)))
		}
	}

	for {
		select {
		case <-a.tickerChan:
			tick()
		case s := <-a.statChan:
			record(s)
		}
	}
}
