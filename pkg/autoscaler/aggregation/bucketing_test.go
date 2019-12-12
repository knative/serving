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
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const pod = "pod"

func TestTimedFloat64Buckets(t *testing.T) {
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
		granularity: time.Second,
		stats: []args{
			{trunc1, pod, 1.0}, // activator scale from 0.
			{trunc1.Add(100 * time.Millisecond), pod, 10.0}, // from scraping pod/sent by activator.
			{trunc1.Add(1 * time.Second), pod, 1.0},         // next bucket
			{trunc1.Add(3 * time.Second), pod, 1.0},         // nextnextnext bucket
		},
		want: map[time.Time]float64{
			trunc1:                      11.0,
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
			trunc5:                      2.0,
			trunc5.Add(5 * time.Second): 1.0,
		},
	}, {
		name:        "empty",
		granularity: time.Second,
		stats:       []args{},
		want:        map[time.Time]float64{},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buckets := NewTimedFloat64Buckets(tt.granularity)
			for _, stat := range tt.stats {
				buckets.Record(stat.time, stat.name, stat.value)
			}

			got := make(map[time.Time]float64)
			for time, bucket := range buckets.buckets {
				got[time] = bucket
			}

			if !cmp.Equal(tt.want, got) {
				t.Errorf("Unexpected values (-want +got): %v", cmp.Diff(tt.want, got))
			}
			if len(tt.want) == 0 && !buckets.isEmpty() {
				t.Error("IsEmpty() = false, want true")
			}

			// New implementation test.
			buckets2 := NewTimedFloat64Buckets2(2*time.Minute, tt.granularity)
			for _, stat := range tt.stats {
				buckets2.Record(stat.time, stat.name, stat.value)
			}

			got = make(map[time.Time]float64)
			// Less time in future than our window is (2mins above), but more than any of the tests report.
			buckets2.ForEachBucket(trunc1.Add(time.Minute), func(t time.Time, b float64Bucket) {
				// Since we're storing 0s when there's no data, we need to exclude those
				// for this test.
				if s := b.sum(); s > 0 {
					got[t] = b.sum()
				}
			})

			if !cmp.Equal(tt.want, got) {
				t.Errorf("Unexpected values (-want +got): %v", cmp.Diff(tt.want, got))
			}
			if len(tt.want) == 0 && !buckets.isEmpty() {
				t.Error("IsEmpty() = false, want true")
			}
		})
	}
}

func TestTimedFloat64BucketsManyPods(t *testing.T) {
	granularity := time.Second
	trunc1 := time.Now().Truncate(granularity)
	buckets := NewTimedFloat64Buckets(granularity)
	buckets2 := NewTimedFloat64Buckets2(time.Minute, granularity)
	buckets3 := NewTimedFloat64Buckets(granularity)
	for p := 0; p < 5; p++ {
		trunc1 = trunc1.Add(granularity)
		for t := 0; t < 5; t++ {
			buckets.Record(trunc1, pod+strconv.Itoa(p), float64(p+t))
			buckets2.Record(trunc1, pod+strconv.Itoa(p), float64(p+t))
			// Report same values but for one pod.
			buckets3.Record(trunc1, pod, float64(p+t))
		}
	}
	// So the buckets are:
	// [0, 1, 2, 3, 4] = 10
	// [1, 2, 3, 4, 5] = 15
	// ...						 = 20, 25
	// [4, 5, 6, 7, 8]  = 30
	//                  = 100 /5 = 20
	sum1, sum2, sum3 := 0., 0., 0.
	buckets.ForEachBucket(trunc1, func(_ time.Time, b float64Bucket) {
		sum1 += b.sum()
	})
	buckets2.ForEachBucket(trunc1, func(_ time.Time, b float64Bucket) {
		sum2 += b.sum()
	})
	buckets3.ForEachBucket(trunc1, func(_ time.Time, b float64Bucket) {
		sum3 += b.sum()
	})
	if got, want := sum1, 20.; got != want {
		t.Errorf("Sum1 = %f, want: %f", got, want)
	}

	if got, want := sum2, 20.; got != want {
		t.Errorf("Sum2 = %f, want: %f", got, want)
	}
	if got, want := sum3, 20.; got != want {
		t.Errorf("Sum3 = %f, want: %f", got, want)
	}
}

func TestTimedFloat64BucketsForEachBucket(t *testing.T) {
	granularity := time.Second
	trunc1 := time.Now().Truncate(granularity)
	buckets := NewTimedFloat64Buckets(granularity)
	buckets2 := NewTimedFloat64Buckets2(2*time.Minute, granularity)

	if buckets.ForEachBucket(func(time time.Time, bucket float64) {}) {
		t.Fatalf("ForEachBucket unexpectedly returned non-empty result")
	}
	// Since we recorded 0 data, even in this implementation no iteration must occur.
	if buckets2.ForEachBucket(trunc1, func(time time.Time, bucket float64) {}) {
		t.Fatalf("ForEachBucket unexpectedly returned non-empty result")
	}

	buckets.Record(trunc1, pod, 10.0)
	buckets.Record(trunc1.Add(1*time.Second), pod, 10.0)
	buckets.Record(trunc1.Add(2*time.Second), pod, 5.0)
	buckets.Record(trunc1.Add(3*time.Second), pod, 5.0)

	buckets2.Record(trunc1, pod, 10.0)
	buckets2.Record(trunc1.Add(1*time.Second), pod, 10.0)
	buckets2.Record(trunc1.Add(2*time.Second), pod, 5.0)
	buckets2.Record(trunc1.Add(3*time.Second), pod, 5.0)

	acc1 := 0
	acc2 := 0
	if !buckets.ForEachBucket(
		func(time.Time, float64) {
			acc1++
		},
		func(time.Time, float64) {
			acc2++
		},
	) {
		t.Fatal("ForEachBucket unexpectedly returned empty result")
	}

	want := 4
	if acc1 != want {
		t.Errorf("acc1 = %v, want %v", acc1, want)
	}
	if acc2 != want {
		t.Errorf("acc2 = %v, want %v", acc1, want)
	}

	// Now verify second impl
	acc1 = 0
	acc2 = 0
	if !buckets2.ForEachBucket(trunc1.Add(4*time.Second),
		func(_ time.Time, b float64) {
			// We need to exclude the 0s for this test.
			if b.sum() > 0 {
				acc1++
			}
		},
		func(_ time.Time, b float64) {
			if b.sum() > 0 {
				acc2++
			}
		},
	) {
		t.Fatal("ForEachBucket unexpectedly returned empty result")
	}

	if acc1 != want {
		t.Errorf("acc1 = %v, want %v", acc1, want)
	}
	if acc2 != want {
		t.Errorf("acc2 = %v, want %v", acc1, want)
	}
}

func TestTimedFloat64BucketsRemoveOlderThan(t *testing.T) {
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
					bucket.record(name, value)
				}
			}

			if got := bucket.sum(); got != tt.want {
				t.Errorf("Average() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimedFloat64BucketsWindowUpdate(t *testing.T) {
	granularity := time.Second
	trunc1 := time.Now().Truncate(granularity)
	buckets := NewTimedFloat64Buckets2(5*time.Second, granularity)

	// Fill the whole bucketing list with rollover.
	buckets.Record(trunc1, pod, 1)
	buckets.Record(trunc1.Add(1*time.Second), pod, 2)
	buckets.Record(trunc1.Add(2*time.Second), pod, 3)
	buckets.Record(trunc1.Add(3*time.Second), pod, 4)
	buckets.Record(trunc1.Add(4*time.Second), pod, 5)
	buckets.Record(trunc1.Add(5*time.Second), pod, 6)
	sum := 0.
	buckets.ForEachBucket(trunc1.Add(5*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	if got, want := sum, float64(2+3+4+5+6); got != want {
		t.Fatalf("Initial data set Sum = %v, want: %v", got, want)
	}

	// Increase window.
	buckets.ResizeWindow(10 * time.Second)
	if got, want := len(buckets.buckets), 10; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}
	if got, want := buckets.window, 10*time.Second; got != want {
		t.Fatalf("Resized bucket windos = %v, want: %v", got, want)
	}

	// Verify values were properly copied.
	sum = 0.
	buckets.ForEachBucket(trunc1.Add(5*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	if got, want := sum, float64(2+3+4+5+6); got != want {
		t.Fatalf("After first resize data set Sum = %v, want: %v", got, want)
	}
	// Add one more. Make sure all the data is preserved, since window is longer.
	buckets.Record(trunc1.Add(6*time.Second), pod, 7)
	sum = 0.
	buckets.ForEachBucket(trunc1.Add(6*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	if got, want := sum, float64(2+3+4+5+6+7); got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}

	// Now let's reduce window size.
	buckets.ResizeWindow(4 * time.Second)
	if got, want := len(buckets.buckets), 4; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}
	// Just last 4 buckets should have remained.
	sum = 0.
	buckets.ForEachBucket(trunc1.Add(6*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	if got, want := sum, float64(4+5+6+7); got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}

	// Verify idempotence.
	ob := &buckets.buckets
	buckets.ResizeWindow(4 * time.Second)
	if ob != &buckets.buckets {
		t.Error("The buckets have changed, though window didn't")
	}
}

func TestTimedFloat64BucketsWindowUpdate3sGranularity(t *testing.T) {
	granularity := 3 * time.Second
	trunc1 := time.Now().Truncate(granularity)

	// So two buckets here (ceil(5/3)=ceil(1.6666666)=2).
	buckets := NewTimedFloat64Buckets2(5*time.Second, granularity)
	if got, want := len(buckets.buckets), 2; got != want {
		t.Fatalf("Initial bucket count = %d, want: %d", got, want)
	}

	// Fill the whole bucketing list.
	buckets.Record(trunc1, pod, 1)
	buckets.Record(trunc1.Add(1*time.Second), pod, 2)
	buckets.Record(trunc1.Add(2*time.Second), pod, 3)
	buckets.Record(trunc1.Add(3*time.Second), pod, 4)
	buckets.Record(trunc1.Add(4*time.Second), pod, 5)
	buckets.Record(trunc1.Add(5*time.Second), pod, 6)
	buckets.Record(trunc1.Add(6*time.Second), pod, 6)
	sum := 0.
	buckets.ForEachBucket(trunc1.Add(6*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	// Note the per bucket averaging.
	if got, want := sum, (4.+5.+6.)/3.+6; got != want {
		t.Fatalf("Initial data set Sum = %v, want: %v", got, want)
	}

	// Increase window.
	buckets.ResizeWindow(10 * time.Second)
	if got, want := len(buckets.buckets), 4; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}
	if got, want := buckets.window, 10*time.Second; got != want {
		t.Fatalf("Resized bucket windos = %v, want: %v", got, want)
	}

	// Verify values were properly copied.
	sum = 0.
	buckets.ForEachBucket(trunc1.Add(5*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	if got, want := sum, float64(2+3+4+5+6); got != want {
		t.Fatalf("After first resize data set Sum = %v, want: %v", got, want)
	}
	// Add one more. Make sure all the data is preserved, since window is longer.
	buckets.Record(trunc1.Add(6*time.Second), pod, 7)
	sum = 0.
	buckets.ForEachBucket(trunc1.Add(6*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	if got, want := sum, float64(2+3+4+5+6+7); got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}

	// Now let's reduce window size.
	buckets.ResizeWindow(4 * time.Second)
	if got, want := len(buckets.buckets), 4; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}
	// Just last 4 buckets should have remained.
	sum = 0.
	buckets.ForEachBucket(trunc1.Add(6*time.Second), func(t time.Time, b float64Bucket) {
		sum += b.sum()
	})
	if got, want := sum, float64(4+5+6+7); got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}

	// Verify idempotence.
	ob := &buckets.buckets
	buckets.ResizeWindow(4 * time.Second)
	if ob != &buckets.buckets {
		t.Error("The buckets have changed, though window didn't")
	}
}
