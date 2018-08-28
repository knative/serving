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
	"testing"
	"time"
)

func TestRetryer(t *testing.T) {
	examples := []struct {
		label       string
		interval    time.Duration
		maxRetries  int
		responses   []bool
		wantRetries int
	}{
		{
			label:       "atleast once",
			maxRetries:  0,
			responses:   []bool{false},
			wantRetries: 1,
		},
		{
			label:       "< maxRetries",
			maxRetries:  3,
			responses:   []bool{true, false},
			wantRetries: 2,
		},
		{
			label:       "= maxRetries",
			maxRetries:  3,
			responses:   []bool{true, true, false},
			wantRetries: 3,
		},
		{
			label:       "> maxRetries",
			maxRetries:  3,
			responses:   []bool{true, true, true, false},
			wantRetries: 3,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			var got int

			a := func() bool {
				ok := e.responses[got]
				got++

				return ok
			}

			lr := NewRetryer(func(_ int) time.Duration { return 0 }, e.maxRetries)

			reported := lr.Retry(a)

			if got != e.wantRetries {
				t.Errorf("Unexpected retries. Want %d, got %d", e.wantRetries, got)
			}

			if reported != e.wantRetries {
				t.Errorf("Unexpected retries reported. Want %d, got %d", e.wantRetries, reported)
			}
		})
	}
}

func TestLinearIntervalFunc(t *testing.T) {
	interval := 100 * time.Millisecond
	examples := []struct {
		label            string
		retries          int
		expectedInterval time.Duration
	}{
		{
			label:            "linear: 1 retry",
			retries:          1,
			expectedInterval: interval,
		},
		{
			label:            "linear: 3 retries",
			retries:          3,
			expectedInterval: interval,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			intervalFunc := NewLinearIntervalFunc(interval)
			got := intervalFunc(e.retries)

			if got != e.expectedInterval {
				t.Errorf("Unexpected interval. Want %d, got %d", e.expectedInterval, got)
			}
		})
	}
}

func TestExponentialIntervalFunc(t *testing.T) {
	minInterval := 100 * time.Millisecond
	examples := []struct {
		label            string
		base             float64
		retries          int
		expectedInterval time.Duration
	}{
		{
			label:            "exponential: 1 retry",
			base:             1.3,
			retries:          1,
			expectedInterval: 130 * time.Millisecond,
		},
		{
			label:            "exponential: 3 retries",
			base:             1.3,
			retries:          3,
			expectedInterval: 219 * time.Millisecond,
		},
		{
			label:            "exponential: 10 retries",
			base:             1.3,
			retries:          10,
			expectedInterval: 1378 * time.Millisecond,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			intervalFunc := NewExponentialIntervalFunc(minInterval, e.base)
			got := intervalFunc(e.retries)

			if got != e.expectedInterval {
				t.Errorf("Unexpected interval. Want %d, got %d", e.expectedInterval, got)
			}
		})
	}
}
