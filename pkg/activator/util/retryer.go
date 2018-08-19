/*
Copyright 2018 The Knative Authors
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

package util

import (
	"math"
	"time"
)

// RetryerFunc is a function that wraps an action to be
// retried.
type RetryerFunc func(ActionFunc) int

// ActionFunc is a function that is retried by a `Retryer`.
// Returns true iff succeeded, false if not.
type ActionFunc func() bool

// IntervalFunc is a function that calculates an interval
// given the number of retries that already happened.
type IntervalFunc func(int) time.Duration

// Retryer is an entity that can retry a given `ActionFunc`.
type Retryer interface {
	Retry(ActionFunc) int
}

// Retry invokes 1 retry on `f`
func (r RetryerFunc) Retry(f ActionFunc) int {
	return r(f)
}

// NewRetryer creates a function where `action` will be retried
// at most `maxRetries` times, with an interval calculated in
// between retries by the `intervalFunc`
func NewRetryer(intervalFunc IntervalFunc, maxRetries int) Retryer {
	return RetryerFunc(func(action ActionFunc) (retries int) {
		for retries = 1; action() && retries < maxRetries; retries++ {
			time.Sleep(intervalFunc(retries))
		}
		return
	})
}

// NewLinearIntervalFunc creates a function always returning
// a static value `interval`
func NewLinearIntervalFunc(interval time.Duration) IntervalFunc {
	return func(_ int) time.Duration {
		return interval
	}
}

// NewExponentialIntervalFunc creates a function returning a
// `time.Duration`, that represents the time to wait in between
// two retries, calculated as `minInterval * (base ^ retries)`
func NewExponentialIntervalFunc(minInterval time.Duration, base float64) IntervalFunc {
	return func(retries int) time.Duration {
		retryIntervalMs := float64(minInterval / time.Millisecond)
		multiplicator := math.Pow(base, float64(retries))
		return time.Duration(int(retryIntervalMs*multiplicator)) * time.Millisecond
	}
}
