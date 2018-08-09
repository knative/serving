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

func TestLinearRetry(t *testing.T) {
	checkInterval := func(last *time.Time, want time.Duration) {
		now := time.Now()
		got := now.Sub(*last)
		*last = now

		if got < want {
			t.Errorf("Unexpected retry interval. Want %v, got %v", want, got)
		}
	}

	examples := []struct {
		label       string
		interval    time.Duration
		maxRetries  int
		responses   []bool
		wantRetries int
	}{
		{
			label:       "atleast once",
			interval:    5 * time.Millisecond,
			maxRetries:  0,
			responses:   []bool{true},
			wantRetries: 1,
		},
		{
			label:       "< maxRetries",
			interval:    5 * time.Millisecond,
			maxRetries:  3,
			responses:   []bool{false, true},
			wantRetries: 2,
		},
		{
			label:       "= maxRetries",
			interval:    10 * time.Millisecond,
			maxRetries:  3,
			responses:   []bool{false, false, true},
			wantRetries: 3,
		},
		{
			label:       "> maxRetries",
			interval:    5 * time.Millisecond,
			maxRetries:  3,
			responses:   []bool{false, false, false, true},
			wantRetries: 3,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			var lastRetry time.Time
			var got int

			a := func() bool {
				checkInterval(&lastRetry, e.interval)

				ok := e.responses[got]
				got++

				return ok
			}

			lr := NewLinearRetryer(e.interval, e.maxRetries)

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
