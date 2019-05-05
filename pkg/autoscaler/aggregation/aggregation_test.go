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
	"testing"
	"time"
)

func TestAverage(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   float64
	}{{
		name:   "empty",
		values: []float64{},
		want:   0.0,
	}, {
		name:   "not empty",
		values: []float64{2.0, 4.0},
		want:   3.0,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			average := Average{}
			for _, value := range tt.values {
				bucket := float64Bucket{}
				bucket.Record("pod", value)
				average.Accumulate(time.Now(), bucket)
			}

			if got := average.Value(); got != tt.want {
				t.Errorf("Value() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestYoungerThan(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(1 * time.Second)
	t2 := t0.Add(2 * time.Second)
	t3 := t0.Add(3 * time.Second)

	tests := []struct {
		name   string
		times  []time.Time
		oldest time.Time
		want   []time.Time
	}{{
		name:  "empty",
		times: []time.Time{},
		want:  []time.Time{},
	}, {
		name:   "drop all",
		times:  []time.Time{t0, t1, t2},
		oldest: t3,
		want:   []time.Time{},
	}, {
		name:   "keep all",
		times:  []time.Time{t0, t1, t2},
		oldest: t0,
		want:   []time.Time{t0, t1, t2},
	}, {
		name:   "drop some",
		times:  []time.Time{t0, t1, t2},
		oldest: t2,
		want:   []time.Time{t2},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(map[time.Time]bool)
			acc := YoungerThan(tt.oldest, func(time time.Time, bucket float64Bucket) {
				got[time] = true
			})
			for _, t := range tt.times {
				bucket := float64Bucket{}
				bucket.Record("pod", 1.0)
				acc(t, bucket)
			}

			if got, want := len(got), len(tt.want); got != want {
				t.Errorf("len(got) = %v, want %v", got, want)
			}

			for _, want := range tt.want {
				if !got[want] {
					t.Errorf("Expected buckets to contain %v, buckets: %v", want, got)
				}
			}
		})
	}
}
