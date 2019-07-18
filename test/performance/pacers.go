/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package performance

import (
	"fmt"
	"math"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"
)

// steadyUpPacer is a Pacer that describes attack request rates that increases in the beginning then becomes steady.
//  Max  |     ,----------------
//       |    /
//       |   /
//       |  /
//       | /
//  Min -+------------------------------> t
//       |<-Up->|
type steadyUpPacer struct {
	// UpDuration is the duration that attack request rates increase from Min to Max.
	UpDuration time.Duration
	// Min is the attack request rates from the beginning, must be larger than 0.
	Min vegeta.Rate
	// Max is the maximum and final steady attack request rates.
	Max vegeta.Rate

	slope        float64
	minHitsPerNs float64
	maxHitsPerNs float64
}

// NewSteadyUpPacer returns a new SteadyUpPacer with the given config.
func NewSteadyUpPacer(min vegeta.Rate, max vegeta.Rate, upDuration time.Duration) vegeta.Pacer {
	return &steadyUpPacer{
		Min:          min,
		Max:          max,
		UpDuration:   upDuration,
		slope:        (hitsPerNs(max) - hitsPerNs(min)) / float64(upDuration),
		minHitsPerNs: hitsPerNs(min),
		maxHitsPerNs: hitsPerNs(max),
	}
}

// steadyUpPacer satisfies the Pacer interface.
var _ vegeta.Pacer = steadyUpPacer{}

// String returns a pretty-printed description of the steadyUpPacer's behaviour.
func (sup steadyUpPacer) String() string {
	return fmt.Sprintf("Up{%s + %s / %s}, then Steady{%s}", sup.Min, sup.Max, sup.UpDuration, sup.Max)
}

// invalid tests the constraints documented in the steadyUpPacer struct definition.
func (sup steadyUpPacer) invalid() bool {
	return sup.UpDuration <= 0 || sup.minHitsPerNs <= 0 || sup.maxHitsPerNs <= sup.minHitsPerNs
}

// Pace determines the length of time to sleep until the next hit is sent.
func (sup steadyUpPacer) Pace(elapsedTime time.Duration, elapsedHits uint64) (time.Duration, bool) {
	if sup.invalid() {
		// If pacer configuration is invalid, stop the attack.
		return 0, true
	}

	expectedHits := sup.hits(elapsedTime)
	if elapsedHits < uint64(expectedHits) {
		// Running behind, send next hit immediately.
		return 0, false
	}

	// Re-arranging our hits equation to provide a duration given the number of
	// requests sent is non-trivial, so we must solve for the duration numerically.
	// math.Round() added here because we have to coerce to int64 nanoseconds
	// at some point and it corrects a bunch of off-by-one problems.
	nsPerHit := 1 / sup.hitsPerNs(elapsedTime)
	hitsToWait := float64(elapsedHits+1) - expectedHits
	nextHitIn := time.Duration(nsPerHit * hitsToWait)

	// If we can't converge to an error of <1e-3 within 10 iterations, bail.
	// This rarely even loops for any large Period if hitsToWait is small.
	for i := 0; i < 10; i++ {
		hitsAtGuess := sup.hits(elapsedTime + nextHitIn)
		err := float64(elapsedHits+1) - hitsAtGuess
		if math.Abs(err) < 1e-3 {
			return nextHitIn, false
		}
		nextHitIn = time.Duration(float64(nextHitIn) / (hitsAtGuess - float64(elapsedHits)))
	}

	return nextHitIn, false
}

// hits returns the number of expected hits for this pacer during the given time.
func (sup steadyUpPacer) hits(t time.Duration) float64 {
	if t <= 0 || sup.invalid() {
		return 0
	}

	// If t is smaller than the UpDuration, calculate the hits as a trapezoid.
	if t <= sup.UpDuration {
		curtHitsPerNs := sup.hitsPerNs(t)
		return (curtHitsPerNs + sup.minHitsPerNs) / 2.0 * float64(t)
	}

	// If t is larger than the UpDuration, calculate the hits as a trapezoid + a rectangle.
	upHits := (sup.maxHitsPerNs + sup.minHitsPerNs) / 2.0 * float64(sup.UpDuration)
	steadyHits := sup.maxHitsPerNs * float64(t-sup.UpDuration)
	return upHits + steadyHits
}

// hitsPerNs returns the attack rate for this pacer at a given time.
func (sup steadyUpPacer) hitsPerNs(t time.Duration) float64 {
	if t <= sup.UpDuration {
		return sup.minHitsPerNs + float64(t)*sup.slope
	}

	return sup.maxHitsPerNs
}

// hitsPerNs returns the attack rate this ConstantPacer represents, in
// fractional hits per nanosecond.
func hitsPerNs(cp vegeta.ConstantPacer) float64 {
	return float64(cp.Freq) / float64(cp.Per)
}
