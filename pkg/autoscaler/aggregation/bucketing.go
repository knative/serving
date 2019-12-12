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

// TimedFloat64Buckets keeps buckets that have been collected at a certain time.
type TimedFloat64Buckets struct {
	bucketsMutex sync.RWMutex
	// Metrics received in a certain timeframe are all summed up.
	// This assumes that we don't take multiple readings of
	// the same metric in the same bucket (per second currently).
	// The only case where this might happen currently is when activator scales
	// a revision from 0. The metrics for that bucket might be off
	// by exactly "1" as that poke always reports a concurrency of 1.
	// Since we're windowing metrics anyway, that slight skew is acceptable.
	buckets map[time.Time]float64

	granularity time.Duration
}

// NewTimedFloat64Buckets generates a new TimedFloat64Buckets with the given
// granularity.
func NewTimedFloat64Buckets(granularity time.Duration) *TimedFloat64Buckets {
	return &TimedFloat64Buckets{
		buckets:     make(map[time.Time]float64),
		granularity: granularity,
	}
}

// Record adds a value with an associated time to the correct bucket.
func (t *TimedFloat64Buckets) Record(time time.Time, name string, value float64) {
	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()

	bucketKey := time.Truncate(t.granularity)
	t.buckets[bucketKey] += value
}

// isEmpty returns whether or not there are no values currently stored.
// isEmpty requires t.bucketMux to be held.
func (t *TimedFloat64Buckets) isEmpty() bool {
	return len(t.buckets) == 0
}

// ForEachBucket calls the given Accumulator function for each bucket.
// Returns true if any data was recorded.
func (t *TimedFloat64Buckets) ForEachBucket(accs ...Accumulator) bool {
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()
	if t.isEmpty() {
		return false
	}

	for bucketTime, bucket := range t.buckets {
		for _, acc := range accs {
			acc(bucketTime, bucket)
		}
	}
	return true
}

// RemoveOlderThan removes buckets older than the given time from the state.
func (t *TimedFloat64Buckets) RemoveOlderThan(time time.Time) {
	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()

	for bucketTime := range t.buckets {
		if bucketTime.Before(time) {
			delete(t.buckets, bucketTime)
		}
	}
}
