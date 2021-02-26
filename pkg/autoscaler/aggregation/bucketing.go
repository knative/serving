/*
Copyright 2019 The Knative Authors

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

package aggregation

import (
	"math"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
)

type (
	// TimedFloat64Buckets keeps buckets that have been collected at a certain time.
	TimedFloat64Buckets struct {
		bucketsMutex sync.RWMutex
		// buckets is a ring buffer indexed by timeToIndex() % len(buckets).
		// Each element represents a certain granularity of time, and the total
		// represented duration adds up to a window length of time.
		buckets []float64

		// firstWrite holds the time when the first write has been made.
		// This time is reset to `now` when the very first write happens,
		// or when a first write happens after `window` time of inactivity.
		// The difference between `now` and `firstWrite` is used to compute
		// the number of eligible buckets for computation of average values.
		firstWrite time.Time

		// lastWrite stores the time when the last write was made.
		// This is used to detect when we have gaps in the data (i.e. more than a
		// granularity has expired since the last write) so that we can zero those
		// entries in the buckets array. It is also used when calculating the
		// WindowAverage to know how much of the buckets array represents valid data.
		lastWrite time.Time

		// granularity is the duration represented by each bucket in the buckets ring buffer.
		granularity time.Duration
		// window is the total time represented by the buckets ring buffer.
		window time.Duration
		// The total sum of all buckets within the window. This total includes
		// invalid buckets, e.g. buckets written to before firstTime or after
		// lastTime are included in this total.
		windowTotal float64
	}

	// WeightedFloat64Buckets is the implementation of buckets, that
	// uses weighted average algorithm.
	WeightedFloat64Buckets struct {
		*TimedFloat64Buckets

		// smoothingCoeff contains the speed with which the importance
		// of items in the past decays. The larger the faster weights decay.
		// It is autocomputed from window size and weightPrecision constant
		// and is bounded by minExponent below.
		smoothingCoeff float64
	}
)

// String implements the Stringer interface.
func (t *TimedFloat64Buckets) String() string {
	return spew.Sdump(t.buckets)
}

// computeSmoothingCoeff computes the decay given number of buckets.
// The function uses precision and min exponent value constants.
func computeSmoothingCoeff(nb float64) float64 {
	return math.Max(
		// Given number of buckets, infer the desired multiplier
		// so that at least weightPrecision sum of buckets is met.
		1-math.Pow(1-weightPrecision, 1/nb),
		// If it's smaller than minExponent — then use minExponent,
		// otherwise with extremely large windows we basically end up
		// very close to the simple average.
		minExponent,
	)
}

// NewTimedFloat64Buckets generates a new TimedFloat64Buckets with the given
// granularity.
func NewTimedFloat64Buckets(window, granularity time.Duration) *TimedFloat64Buckets {
	// Number of buckets is `window` divided by `granularity`, rounded up.
	// e.g. 60s / 2s = 30.
	nb := math.Ceil(float64(window) / float64(granularity))
	return &TimedFloat64Buckets{
		buckets:     make([]float64, int(nb)),
		granularity: granularity,
		window:      window,
	}
}

// NewWeightedFloat64Buckets generates a new WeightedFloat64Buckets with the given
// granularity.
func NewWeightedFloat64Buckets(window, granularity time.Duration) *WeightedFloat64Buckets {
	// Number of buckets is `window` divided by `granularity`, rounded up.
	// e.g. 60s / 2s = 30.
	nb := math.Ceil(float64(window) / float64(granularity))
	return &WeightedFloat64Buckets{
		TimedFloat64Buckets: NewTimedFloat64Buckets(window, granularity),
		smoothingCoeff:      computeSmoothingCoeff(nb),
	}
}

// IsEmpty returns true if no data has been recorded for the `window` period.
func (t *TimedFloat64Buckets) IsEmpty(now time.Time) bool {
	now = now.Truncate(t.granularity)
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()
	return t.isEmptyLocked(now)
}

// isEmptyLocked expects `now` to be truncated and at least Read Lock held.
func (t *TimedFloat64Buckets) isEmptyLocked(now time.Time) bool {
	return now.Sub(t.lastWrite) > t.window
}

func roundToNDigits(n int, f float64) float64 {
	p := math.Pow10(n)
	return math.Floor(f*p) / p
}

const (
	// minExponent is the minimal decay multiplier for the weighted average computations.
	minExponent = 0.2
	// The sum of weights for weighted average should be at least this much.
	weightPrecision = 0.9999
	precision       = 6
)

// WindowAverage returns the exponential weighted average. This means
// that more recent items have much greater impact on the average than
// the older ones.
// TODO(vagababov): optimize for O(1) computation, if possible.
// E.g. with data  [10, 10, 5, 5] (newest last), then
// the `WindowAverage` would return (10+10+5+5)/4 = 7.5
// This with exponent of 0.6 would return 5*0.6+5*0.6*0.4+10*0.6*0.4^2+10*0.6*0.4^3 = 5.544
// If we reverse the data to [5, 5, 10, 10] the simple average would remain the same,
// but this one would change to 9.072.
func (t *WeightedFloat64Buckets) WindowAverage(now time.Time) float64 {
	now = now.Truncate(t.granularity)
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()
	if t.isEmptyLocked(now) {
		return 0
	}

	totalB := len(t.buckets)
	numB := len(t.buckets)

	multiplier := t.smoothingCoeff
	// We start with 0es. But we know that we have _some_ data because
	// IsEmpty returned false.
	if now.After(t.lastWrite) {
		numZ := now.Sub(t.lastWrite) / t.granularity
		// Skip to this multiplier directly: m*(1-m)^(nz-1).
		multiplier = multiplier * math.Pow(1-t.smoothingCoeff, float64(numZ))
		// Reduce effective number of buckets.
		numB -= int(numZ)
	}
	startIdx := t.timeToIndex(t.lastWrite) + totalB // To ensure always positive % operation.
	ret := 0.
	for i := 0; i < numB; i++ {
		effectiveIdx := (startIdx - i) % totalB
		v := t.buckets[effectiveIdx] * multiplier
		ret += v
		multiplier *= (1 - t.smoothingCoeff)
		// TODO(vagababov): bail out if sm > weightPrecision?
	}
	return ret
}

// WindowAverage returns the average bucket value over the window.
//
// If the first write was less than the window length ago, an average is
// returned over the partial window. For example, if firstWrite was 6 seconds
// ago, the average will be over these 6 seconds worth of buckets, even if the
// window is 60s. If a window passes with no data being received, the first
// write time is reset so this behaviour takes effect again.
//
// Similarly, if we have not received recent data, the average is based on a
// partial window. For example, if the window is 60 seconds but we last
// received data 10 seconds ago, the window average will be the average over
// the first 50 seconds.
//
// In other cases, for example if there are gaps in the data shorter than the
// window length, the missing data is assumed to be 0 and the average is over
// the whole window length inclusive of the missing data.
func (t *TimedFloat64Buckets) WindowAverage(now time.Time) float64 {
	now = now.Truncate(t.granularity)
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()
	switch d := now.Sub(t.lastWrite); {
	case d <= 0:
		// If LastWrite equal or greater than Now
		// return the current WindowTotal, divided by the
		// number of valid buckets
		numB := math.Min(
			float64(t.lastWrite.Sub(t.firstWrite)/t.granularity)+1, // +1 since the times are inclusive.
			float64(len(t.buckets)))
		return roundToNDigits(precision, t.windowTotal/numB)
	case d < t.window:
		// If we haven't received metrics for some time, which is less than
		// the window -- remove the outdated items and divide by the number
		// of valid buckets
		stIdx := t.timeToIndex(t.lastWrite)
		eIdx := t.timeToIndex(now)
		ret := t.windowTotal
		for i := stIdx + 1; i <= eIdx; i++ {
			ret -= t.buckets[i%len(t.buckets)]
		}
		numB := math.Min(
			float64(t.lastWrite.Sub(t.firstWrite)/t.granularity)+1, // +1 since the times are inclusive.
			float64(len(t.buckets)-(eIdx-stIdx)))
		return roundToNDigits(precision, ret/numB)
	default: // Nothing for more than a window time, just 0.
		return 0.
	}
}

// timeToIndex converts time to an integer that can be used for modulo
// operations to find the index in the bucket list.
// bucketMutex needs to be held.
func (t *TimedFloat64Buckets) timeToIndex(tm time.Time) int {
	// I don't think this run in 2038 :-)
	// NB: we need to divide by granularity, since it's a compressing mapping
	// to buckets.
	return int(tm.Unix()) / int(t.granularity.Seconds())
}

// Record adds a value with an associated time to the correct bucket.
// If this record would introduce a gap in the data, any intervening times
// between the last write and this one will be recorded as zero. If an entire
// window length has expired without data, the firstWrite time is reset,
// meaning the WindowAverage will be of a partial window until enough data is
// received to fill it again.
func (t *TimedFloat64Buckets) Record(now time.Time, value float64) {
	bucketTime := now.Truncate(t.granularity)

	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()

	writeIdx := t.timeToIndex(now)

	if t.lastWrite != bucketTime {
		if bucketTime.Add(t.window).After(t.lastWrite) {
			// If it is the first write or it happened before the first write which we
			// have in record, update the firstWrite.
			if t.firstWrite.IsZero() || t.firstWrite.After(bucketTime) {
				t.firstWrite = bucketTime
			}

			if bucketTime.After(t.lastWrite) {
				if bucketTime.Sub(t.lastWrite) >= t.window {
					// This means we had no writes for the duration of `window`. So reset the firstWrite time.
					t.firstWrite = bucketTime
					// Reset all the buckets.
					for i := range t.buckets {
						t.buckets[i] = 0
					}
					t.windowTotal = 0
				} else {
					// In theory we might lose buckets between stats gathering.
					// Thus we need to clean not only the current index, but also
					// all the ones from the last write. This is slower than the loop above
					// due to possible wrap-around, so they are not merged together.
					for i := t.timeToIndex(t.lastWrite) + 1; i <= writeIdx; i++ {
						idx := i % len(t.buckets)
						t.windowTotal -= t.buckets[idx]
						t.buckets[idx] = 0
					}
				}
				// Update the last write time.
				t.lastWrite = bucketTime
			}
			// The else case is t.lastWrite - t.window < bucketTime < t.lastWrite, we can simply add
			// the value to the bucket.
		} else {
			// Ignore this value because it happened a window size ago.
			return
		}
	}
	t.buckets[writeIdx%len(t.buckets)] += value
	t.windowTotal += value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ResizeWindow implements window resizing for the weighted averaging buckets object.
func (t *WeightedFloat64Buckets) ResizeWindow(w time.Duration) {
	t.TimedFloat64Buckets.ResizeWindow(w)
	t.smoothingCoeff = computeSmoothingCoeff(math.Ceil(float64(w) / float64(t.granularity)))
}

// ResizeWindow resizes the window. This is an O(N) operation,
// and is not supposed to be executed very often.
func (t *TimedFloat64Buckets) ResizeWindow(w time.Duration) {
	// Same window size, bail out.
	sameWindow := func() bool {
		t.bucketsMutex.RLock()
		defer t.bucketsMutex.RUnlock()
		return w == t.window
	}()
	if sameWindow {
		return
	}
	numBuckets := int(math.Ceil(float64(w) / float64(t.granularity)))
	newBuckets := make([]float64, numBuckets)
	newTotal := 0.

	// We need write lock here.
	// So that we can copy the existing buckets into the new array.
	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()
	// If we had written any data within `window` time, then exercise the O(N)
	// copy algorithm. Otherwise, just assign zeroes.
	if time.Now().Truncate(t.granularity).Sub(t.lastWrite) <= t.window {
		// If the window is shrinking, then we need to copy only
		// `newBuckets` buckets.
		oldNumBuckets := len(t.buckets)
		tIdx := t.timeToIndex(t.lastWrite)
		for i := 0; i < min(numBuckets, oldNumBuckets); i++ {
			oi := tIdx % oldNumBuckets
			ni := tIdx % numBuckets
			newBuckets[ni] = t.buckets[oi]
			// In case we're shrinking, make sure the total
			// window sum will match. This is no-op in case if
			// window is getting bigger.
			newTotal += t.buckets[oi]
			tIdx--
		}
		// We can reset this as well to the earliest well known time when we might have
		// written data, if it is
		t.firstWrite = t.lastWrite.Add(-time.Duration(oldNumBuckets-1) * t.granularity)
	} else {
		// No valid data so far, so reset to initial value.
		t.firstWrite = time.Time{}
	}
	t.window = w
	t.buckets = newBuckets
	t.windowTotal = newTotal
}
