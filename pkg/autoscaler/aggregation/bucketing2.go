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
	"math"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
)

// TimedFloat64Buckets2 keeps buckets that have been collected at a certain time.
type TimedFloat64Buckets2 struct {
	bucketsMutex sync.RWMutex
	buckets      []float64
	lastWrite    time.Time

	granularity time.Duration
	window      time.Duration
}

func (t *TimedFloat64Buckets2) String() string {
	return spew.Sdump(t.buckets)
}

// NewTimedFloat64Buckets2 generates a new TimedFloat64Buckets2 with the given
// granularity.
func NewTimedFloat64Buckets2(window, granularity time.Duration) *TimedFloat64Buckets2 {
	// Number of buckets is `window` divided by `granularity`, rounded up.
	// e.g. 60s / 2s = 30.
	nb := int(math.Ceil(float64(window) / float64(granularity)))
	return &TimedFloat64Buckets2{
		buckets:     make([]float64, nb),
		granularity: granularity,
		window:      window,
	}
}

// timeToIndex converts time to an integer that can be used for modulo
// operations to find the index in the bucket list.
// bucketMutex needs to be held.
func (t *TimedFloat64Buckets2) timeToIndex(tm time.Time) int {
	// I don't think this run in 2038 :-)
	return int(tm.Unix()) / int(t.granularity.Seconds())
}

func (t *TimedFloat64Buckets2) reset() {
	for i := range t.buckets {
		t.buckets[i] = 0
	}
}

// Record adds a value with an associated time to the correct bucket.
func (t *TimedFloat64Buckets2) Record(now time.Time, name string, value float64) {
	bucketTime := now.Truncate(t.granularity)

	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()

	// NB: we need to divide by granularity, since it's a compressing mapping
	// to buckets.
	bucketIdx := t.timeToIndex(now) % len(t.buckets)

	if t.lastWrite != bucketTime {
		// This should not really happen, but is here for correctness.
		if bucketTime.Sub(t.lastWrite) > t.window {
			t.reset()
		}
		// Reset the value.
		t.buckets[bucketIdx] = 0
		// Update the last write time.
		t.lastWrite = bucketTime
	}
	t.buckets[bucketIdx] += value
}

// ForEachBucket calls the given Accumulator function for each bucket.
// Returns true if any data was recorded.
func (t *TimedFloat64Buckets2) ForEachBucket(now time.Time, accs ...Accumulator) bool {
	now = now.Truncate(t.granularity)
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()

	if now.Sub(t.lastWrite) >= t.window {
		return false
	}

	// So number of buckets we can process is len(buckets)-(now-lastWrite)/granularity.
	// Since isEmpty returned false, we know this is at least 1 bucket.
	numBuckets := len(t.buckets) - int(now.Sub(t.lastWrite)/t.granularity)
	bucketTime := t.lastWrite // Always aligned with granularity.
	si := t.timeToIndex(bucketTime)
	for i := 0; i < numBuckets; i++ {
		tix := si % len(t.buckets)
		for _, acc := range accs {
			acc(bucketTime, t.buckets[tix])
		}
		si--
		bucketTime = bucketTime.Add(-t.granularity)
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

// ResizeWindow resizes the window. This is an O(N) operation,
// and is not supposed to be executed very often.
func (t *TimedFloat64Buckets2) ResizeWindow(w time.Duration) {
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

	// We need write lock here.
	// So that we can copy the existing buckets into the new array.
	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()
	// If the window is shrinking, then we need to copy only
	// `nb` buckets.
	oldNumBuckets := len(t.buckets)
	tIdx := t.timeToIndex(t.lastWrite)
	for i := 0; i < min(numBuckets, oldNumBuckets); i++ {
		oi := tIdx % oldNumBuckets
		ni := tIdx % numBuckets
		newBuckets[ni] = t.buckets[oi]
		tIdx--
	}
	t.window = w
	t.buckets = newBuckets
}
