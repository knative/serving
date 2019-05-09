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

import "time"

// Accumulator is a function accumulating buckets and their time..
type Accumulator func(time time.Time, bucket float64Bucket)

// YoungerThan only applies the accumulator to buckets that are younger than the given
// time.
func YoungerThan(oldest time.Time, acc Accumulator) Accumulator {
	return func(time time.Time, bucket float64Bucket) {
		if !time.Before(oldest) {
			acc(time, bucket)
		}
	}
}

// Average is used to keep the values necessary to compute an average.
type Average struct {
	sum   float64
	count float64
}

// Accumulate accumulates the values needed to compute an average.
func (a *Average) Accumulate(_ time.Time, bucket float64Bucket) {
	a.sum += bucket.Sum()
	a.count++
}

// Value returns the average or 0 if no buckets have been accumulated.
func (a *Average) Value() float64 {
	if a.count == 0 {
		return 0
	}
	return a.sum / a.count
}
