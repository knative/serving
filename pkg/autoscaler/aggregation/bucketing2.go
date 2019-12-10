/*
Copyright 2019 The Knative Authors.

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
	"sync"
	"time"
)

// TimedFloat64Buckets2 keeps buckets that have been collected at a certain time.
type TimedFloat64Buckets2 struct {
	bucketsMutex sync.RWMutex
	buckets      []float64Bucket
	lastWrite    time.Time

	granularity time.Duration
	window      time.Duration
}

// NewTimedFloat64Buckets2 generates a new TimedFloat64Buckets2 with the given
// granularity.
func NewTimedFloat64Buckets2(window, granularity time.Duration) *TimedFloat64Buckets2 {
	// Number of buckets is window divided by granurality.
	// e.g. 60s / 2s = 30.
	nb := int(window / granularity)
	return &TimedFloat64Buckets2{
		buckets:     make([]float64Bucket, nb),
		granularity: granularity,
		window:      window,
	}
}

// Record adds a value with an associated time to the correct bucket.
func (t *TimedFloat64Buckets2) Record(now time.Time, name string, value float64) {
	bucketTime := now.Truncate(t.granularity)
	// I don't think this run in 2038 :-)
	bucketIdx := int(bucketTime.Unix()) % len(t.buckets)

	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()

	if t.lastWrite != bucketTime {
		// Reset the value.
		t.buckets[bucketIdx] = float64Bucket{}
		// Update the last write time.
		t.lastWrite = bucketTime
	}
	t.buckets[bucketIdx].record(name, value)
}

// isEmpty returns whether or not there are no values currently stored.
// isEmpty requires t.bucketMux to be held.
func (t *TimedFloat64Buckets2) isEmpty(now time.Time) bool {
	now = now.Truncate(t.granularity)
	return now.Sub(t.lastWrite) >= t.window
}

// ForEachBucket calls the given Accumulator function for each bucket.
// Returns true if any data was recorded.
func (t *TimedFloat64Buckets2) ForEachBucket(now time.Time, accs ...Accumulator) bool {
	now = now.Truncate(t.granularity)
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()

	if t.isEmpty(now) {
		return false
	}

	// So number of buckets we can process is len(buckets)-(now-lastWrite)/granularity.
	// Since isEmpty returned false, we know this is at least 1 bucket.
	nb := len(t.buckets) - int(now.Sub(t.lastWrite)/t.granularity)
	tb := t.lastWrite // Always aligned with granularity.
	for i := 0; i < nb; i++ {
		ti := int(tb.Unix()) % len(t.buckets)
		for _, acc := range accs {
			acc(tb, t.buckets[ti])
		}
		tb = tb.Add(-t.granularity)
	}

	return true
}

// RemoveOlderThan removes buckets older than the given time from the state.
func (t *TimedFloat64Buckets2) RemoveOlderThan(time.Time) {
	// RemoveOlderThan is a noop here.
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (t *TimedFloat64Buckets2) ResizeWindow(w time.Duration) {
	// Same window size, bail out.
	if func() bool {
		t.bucketsMutex.RLock()
		defer t.bucketsMutex.RUnlock()
		return w == t.window
	}() {
		return
	}
	nb := int(w / t.granularity)
	newb := make([]float64Bucket, nb)

	// We need write lock here.
	// So that we can copy the existing buckets into the new array.
	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()
	// If the window is shrinking, then we need to copy only
	// `nb` buckets.
	onb := len(t.buckets)
	tIdx := int(t.lastWrite.Unix())
	for i := 0; i < min(nb, onb); i++ {
		oi := tIdx % onb
		ni := tIdx % nb
		newb[ni] = t.buckets[oi]
		tIdx -= int(t.granularity.Seconds())
	}
	t.window = w
	t.buckets = newb
}
