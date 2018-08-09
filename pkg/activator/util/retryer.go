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

import "time"

type Retryer interface {
	Retry(func() bool) int
}

type ActionFunc func() bool
type RetryerFunc func(ActionFunc) int

func (r RetryerFunc) Retry(f func() bool) int {
	return r(f)
}

// NewLinearRetryer will return a retryer that retries `action` up to
// `maxRetries` times with `interval` delay between retries
func NewLinearRetryer(interval time.Duration, maxRetries int) Retryer {
	return RetryerFunc(func(action ActionFunc) (retries int) {
		for retries = 1; !action() && retries < maxRetries; retries++ {
			time.Sleep(interval)
		}
		return
	})
}
