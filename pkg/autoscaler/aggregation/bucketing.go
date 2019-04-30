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
	buckets      map[time.Time]float64Bucket

	granularity time.Duration
}

// NewTimedFloat64Buckets generates a new TimedFloat64Buckets with the given
// granularity.
func NewTimedFloat64Buckets(granularity time.Duration) *TimedFloat64Buckets {
	return &TimedFloat64Buckets{
		buckets:     make(map[time.Time]float64Bucket),
		granularity: granularity,
	}
}

// Record adds a value with an associated time to the correct bucket.
func (t *TimedFloat64Buckets) Record(time time.Time, name string, value float64) {
	t.bucketsMutex.Lock()
	defer t.bucketsMutex.Unlock()

	bucketKey := time.Truncate(t.granularity)
	bucket, ok := t.buckets[bucketKey]
	if !ok {
		bucket = float64Bucket{}
		t.buckets[bucketKey] = bucket
	}
	bucket.Record(name, value)
}

// IsEmpty returns whether or not there are no values currently stored.
func (t *TimedFloat64Buckets) IsEmpty() bool {
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()

	return len(t.buckets) == 0
}

// ForEachBucket calls the given Accumulator function for each bucket.
func (t *TimedFloat64Buckets) ForEachBucket(accs ...Accumulator) {
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()

	for bucketTime, bucket := range t.buckets {
		for _, acc := range accs {
			acc(bucketTime, bucket)
		}
	}
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

// float64Bucket keeps all the stats that fall into a defined bucket.
type float64Bucket map[string]float64Value

// float64Value is a single value for a Float64Bucket. It maintains a summed
// up value and a count to ultimately calculate an average.
type float64Value struct {
	sum   float64
	count float64
}

// Record adds a value to the bucket. Buckets with the same given name
// will be collapsed.
func (b float64Bucket) Record(name string, value float64) {
	current := b[name]
	b[name] = float64Value{
		sum:   current.sum + value,
		count: current.count + 1.0,
	}
}

// Sum calculates the sum over the bucket. Values of the same name in
// the same bucket will be averaged between themselves first.
func (b float64Bucket) Sum() float64 {
	var total float64
	for _, value := range b {
		total += value.sum / value.count
	}
	return total
}
