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

	"github.com/google/go-cmp/cmp"
)

func TestTimedFloat64Buckets(t *testing.T) {
	pod := "pod"
	zero := time.Now()
	trunc1 := zero.Truncate(1 * time.Second)
	trunc5 := zero.Truncate(5 * time.Second)

	type args struct {
		time  time.Time
		name  string
		value float64
	}
	tests := []struct {
		name        string
		granularity time.Duration
		stats       []args
		want        map[time.Time]float64
	}{{
		name:        "granularity = 1s",
		granularity: 1 * time.Second,
		stats: []args{
			{zero, pod, 1.0},
			{zero.Add(100 * time.Millisecond), pod, 1.0}, // same bucket
			{zero.Add(1 * time.Second), pod, 1.0},        // next bucket
			{zero.Add(3 * time.Second), pod, 1.0},        // nextnextnext bucket
		},
		want: map[time.Time]float64{
			trunc1:                      1.0,
			trunc1.Add(1 * time.Second): 1.0,
			trunc1.Add(3 * time.Second): 1.0,
		},
	}, {
		name:        "granularity = 5s",
		granularity: 5 * time.Second,
		stats: []args{
			{zero, pod, 1.0},
			{zero.Add(3 * time.Second), pod, 1.0}, // same bucket
			{zero.Add(6 * time.Second), pod, 1.0}, // next bucket
		},
		want: map[time.Time]float64{
			trunc5:                      1.0,
			trunc5.Add(5 * time.Second): 1.0,
		},
	}, {
		name:  "empty",
		stats: []args{},
		want:  map[time.Time]float64{},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buckets := NewTimedFloat64Buckets(tt.granularity)
			for _, stat := range tt.stats {
				buckets.Record(stat.time, stat.name, stat.value)
			}

			got := make(map[time.Time]float64)
			for time, bucket := range buckets.GetAndLock() {
				got[time] = bucket.Sum()
			}
			buckets.Unlock()

			if !cmp.Equal(tt.want, got) {
				t.Errorf("Unexpected values (-want +got): %v", cmp.Diff(tt.want, got))
			}
			if len(tt.want) == 0 && !buckets.IsEmpty() {
				t.Error("IsEmpty() = false, want true")
			}
		})
	}
}

func TestFloat64Bucket(t *testing.T) {
	tests := []struct {
		name  string
		stats map[string][]float64
		want  float64
	}{{
		name: "sum of value",
		stats: map[string][]float64{
			"test1": {1.0},
			"test2": {2.0},
			"test3": {3.0},
		},
		want: 6.0,
	}, {
		name: "average same first",
		stats: map[string][]float64{
			"test1": {1.0, 8.0},      // average = 4.5
			"test2": {1.0, 3.0, 5.0}, // average = 3
		},
		want: 7.5,
	}, {
		name:  "no values",
		stats: map[string][]float64{},
		want:  0.0,
	}, {
		name: "only zeroes",
		stats: map[string][]float64{
			"test1": {0.0, 0.0},
			"test2": {0.0},
		},
		want: 0.0,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := Float64Bucket{}
			for name, values := range tt.stats {
				for _, value := range values {
					bucket.Record(name, value)
				}
			}

			if got := bucket.Sum(); got != tt.want {
				t.Errorf("Average() = %v, want %v", got, tt.want)
			}
		})
	}
}
