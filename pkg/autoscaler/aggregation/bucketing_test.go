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
	trunc1 := time.Now().Truncate(1 * time.Second)
	trunc5 := time.Now().Truncate(5 * time.Second)

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
			{trunc1, pod, 1.0},
			{trunc1.Add(100 * time.Millisecond), pod, 1.0}, // same bucket
			{trunc1.Add(1 * time.Second), pod, 1.0},        // next bucket
			{trunc1.Add(3 * time.Second), pod, 1.0},        // nextnextnext bucket
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
			{trunc5, pod, 1.0},
			{trunc5.Add(3 * time.Second), pod, 1.0}, // same bucket
			{trunc5.Add(6 * time.Second), pod, 1.0}, // next bucket
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
			for time, bucket := range buckets.buckets {
				got[time] = bucket.Sum()
			}

			if !cmp.Equal(tt.want, got) {
				t.Errorf("Unexpected values (-want +got): %v", cmp.Diff(tt.want, got))
			}
			if len(tt.want) == 0 && !buckets.IsEmpty() {
				t.Error("IsEmpty() = false, want true")
			}
		})
	}
}

func TestTimedFloat64Buckets_ForEachBucket(t *testing.T) {
	pod := "pod"
	granularity := 1 * time.Second
	trunc1 := time.Now().Truncate(granularity)
	buckets := NewTimedFloat64Buckets(granularity)

	buckets.Record(trunc1, pod, 10.0)
	buckets.Record(trunc1.Add(1*time.Second), pod, 10.0)
	buckets.Record(trunc1.Add(2*time.Second), pod, 5.0)
	buckets.Record(trunc1.Add(3*time.Second), pod, 5.0)

	acc1 := 0
	acc2 := 0
	buckets.ForEachBucket(
		func(time time.Time, bucket float64Bucket) {
			acc1++
		},
		func(time time.Time, bucket float64Bucket) {
			acc2++
		},
	)

	want := 4
	if acc1 != want {
		t.Errorf("acc1 = %v, want %v", acc1, want)
	}
	if acc2 != want {
		t.Errorf("acc2 = %v, want %v", acc1, want)
	}
}

func TestTimedFloat64Buckets_RemoveOlderThan(t *testing.T) {
	pod := "pod"
	zero := time.Now()
	trunc1 := zero.Truncate(1 * time.Second)

	tests := []struct {
		name            string
		granularity     time.Duration
		times           []time.Time
		removeOlderThan time.Time
		want            []time.Time
	}{{
		name:        "remove one",
		granularity: 1 * time.Second,
		times: []time.Time{
			trunc1,
			trunc1.Add(1 * time.Second),
			trunc1.Add(2 * time.Second),
		},
		removeOlderThan: trunc1.Add(1 * time.Second),
		want: []time.Time{
			trunc1.Add(1 * time.Second),
			trunc1.Add(2 * time.Second),
		},
	}, {
		name:        "remove all",
		granularity: 1 * time.Second,
		times: []time.Time{
			trunc1,
			trunc1.Add(1 * time.Second),
		},
		removeOlderThan: trunc1.Add(2 * time.Second),
		want:            []time.Time{},
	}, {
		name:        "remove none",
		granularity: 1 * time.Second,
		times: []time.Time{
			trunc1,
			trunc1.Add(1 * time.Second),
		},
		removeOlderThan: trunc1,
		want: []time.Time{
			trunc1,
			trunc1.Add(1 * time.Second),
		},
	}, {
		name:            "empty",
		granularity:     1 * time.Second,
		times:           []time.Time{},
		removeOlderThan: trunc1.Add(1 * time.Second),
		want:            []time.Time{},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buckets := NewTimedFloat64Buckets(tt.granularity)
			for _, time := range tt.times {
				buckets.Record(time, pod, 1.0)
			}

			if got, want := len(buckets.buckets), len(tt.times); got != want {
				t.Errorf("len(buckets) = %v, want %v", got, want)
			}

			buckets.RemoveOlderThan(tt.removeOlderThan)

			if got, want := len(buckets.buckets), len(tt.want); got != want {
				t.Errorf("len(buckets) = %v, want %v", got, want)
			}

			got := make(map[time.Time]bool)
			for time := range buckets.buckets {
				got[time] = true
			}
			for _, want := range tt.want {
				if !got[want] {
					t.Errorf("Expected buckets to contain %v, buckets: %v", want, got)
				}
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
			bucket := float64Bucket{}
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
