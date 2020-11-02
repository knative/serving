/*
Copyright 2019 The Knative Authors

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
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	granularity = time.Second
)

func TestTimedFloat64BucketsSimple(t *testing.T) {
	trunc1 := time.Now().Truncate(1 * time.Second)
	trunc5 := time.Now().Truncate(5 * time.Second)

	type args struct {
		time  time.Time
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
			{trunc1, 1.0}, // activator scale from 0.
			{trunc1.Add(100 * time.Millisecond), 10.0}, // from scraping pod/sent by activator.
			{trunc1.Add(1 * time.Second), 1.0},         // next bucket
			{trunc1.Add(3 * time.Second), 1.0},         // nextnextnext bucket
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
			{trunc5, 1.0},
			{trunc5.Add(3 * time.Second), 11.0}, // same bucket
			{trunc5.Add(6 * time.Second), 1.0},  // next bucket
		},
		want: map[time.Time]float64{
			trunc5:                      12.0,
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
			// New implementation test.
			buckets := NewTimedFloat64Buckets(2*time.Minute, tt.granularity)
			if !buckets.IsEmpty(trunc1) {
				t.Error("Unexpected non empty result")
			}
			for _, stat := range tt.stats {
				buckets.Record(stat.time, stat.value)
			}

			got := make(map[time.Time]float64)
			// Less time in future than our window is (2mins above), but more than any of the tests report.
			buckets.forEachBucket(trunc1.Add(time.Minute), func(t time.Time, b float64) {
				// Since we're storing 0s when there's no data, we need to exclude those
				// for this test.
				if b > 0 {
					got[t] = b
				}
			})

			if !cmp.Equal(tt.want, got) {
				t.Error("Unexpected values (-want +got):", cmp.Diff(tt.want, got))
			}
		})
	}
}

func TestTimedFloat64BucketsManyReps(t *testing.T) {
	trunc1 := time.Now().Truncate(granularity)
	buckets := NewTimedFloat64Buckets(time.Minute, granularity)
	for p := 0; p < 5; p++ {
		trunc1 = trunc1.Add(granularity)
		for t := 0; t < 5; t++ {
			buckets.Record(trunc1, float64(p+t))
		}
	}
	// So the buckets are:
	// t0: [0, 1, 2, 3, 4] = 10
	// t1: [1, 2, 3, 4, 5] = 15
	// t2: [2, 3, 4, 5, 6] = 20
	// t3: [3, 4, 5, 6, 7] = 25
	// t4: [4, 5, 6, 7, 8] = 30
	//                     = 100
	const want = 100.
	sum1, sum2 := 0., 0.
	buckets.forEachBucket(trunc1, func(_ time.Time, b float64) {
		sum1 += b
	})
	buckets.forEachBucket(trunc1, func(_ time.Time, b float64) {
		sum2 += b
	})
	if got, want := sum1, want; got != want {
		t.Errorf("Sum1 = %f, want: %f", got, want)
	}

	if got, want := sum2, want; got != want {
		t.Errorf("Sum2 = %f, want: %f", got, want)
	}
}

func TestTimedFloat64BucketsManyRepsWithNonMonotonicalOrder(t *testing.T) {
	start := time.Now().Truncate(granularity)
	end := start
	buckets := NewTimedFloat64Buckets(time.Minute, granularity)

	d := []int{0, 3, 2, 1, 4}
	for p := 0; p < 5; p++ {
		end = start.Add(time.Duration(d[p]) * granularity)
		for t := 0; t < 5; t++ {
			buckets.Record(end, float64(p+t))
		}
	}

	// So the buckets are:
	// t0: [0, 1, 2, 3, 4] = 10
	// t1: [3, 4, 5, 6, 7] = 25
	// t2: [2, 3, 4, 5, 6] = 20
	// t3: [1, 2, 3, 4, 5] = 15
	// t4: [4, 5, 6, 7, 8] = 30
	//                     = 100
	const want = 100.
	sum1, sum2 := 0., 0.
	buckets.forEachBucket(end, func(_ time.Time, b float64) {
		sum1 += b
	})
	buckets.forEachBucket(end, func(_ time.Time, b float64) {
		sum2 += b
	})
	if got, want := sum1, want; got != want {
		t.Errorf("Sum1 = %f, want: %f", got, want)
	}

	if got, want := sum2, want; got != want {
		t.Errorf("Sum2 = %f, want: %f", got, want)
	}
}

func TestTimedFloat64BucketsWindowAverage(t *testing.T) {
	now := time.Now()
	buckets := NewTimedFloat64Buckets(5*time.Second, granularity)

	// This verifies that we properly use firstWrite. Without that we'd get 0.2.
	buckets.Record(now, 1)
	if got, want := buckets.WindowAverage(now), 1.; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}
	for i := 1; i < 5; i++ {
		buckets.Record(now.Add(time.Duration(i)*time.Second), float64(i+1))
	}

	if got, want := buckets.WindowAverage(now.Add(4*time.Second)), 15./5; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}
	// Check when `now` lags behind.
	if got, want := buckets.WindowAverage(now.Add(3600*time.Millisecond)), 15./5; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// Check with short hole.
	if got, want := buckets.WindowAverage(now.Add(6*time.Second)), (15.-1-2)/(5-2); got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// Check with a long hole.
	if got, want := buckets.WindowAverage(now.Add(10*time.Second)), 0.; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// Check write with holes.
	buckets.Record(now.Add(6*time.Second), 91)
	if got, want := buckets.WindowAverage(now.Add(6*time.Second)), (15.-1-2+91)/5; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// Advance much farther.
	now = now.Add(time.Minute)
	buckets.Record(now, 1984)
	if got, want := buckets.WindowAverage(now), 1984.; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// Check with an earlier time.
	buckets.Record(now.Add(-3*time.Second), 4)
	if got, want := buckets.WindowAverage(now), (4.+1984)/4; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// One more second pass.
	now = now.Add(time.Second)
	buckets.Record(now, 5)
	if got, want := buckets.WindowAverage(now), (4.+1984+5)/5; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// Insert an earlier time again.
	buckets.Record(now.Add(-3*time.Second), 10)
	if got, want := buckets.WindowAverage(now), (4.+10+1984+5)/5; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}

	// Verify that we ignore the value which is too early.
	buckets.Record(now.Add(-6*time.Second), 10)
	if got, want := buckets.WindowAverage(now), (4.+10+1984+5)/5; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}
}

func TestDescendingRecord(t *testing.T) {
	now := time.Now()
	buckets := NewTimedFloat64Buckets(5*time.Second, 1*time.Second)

	for i := 8 * time.Second; i >= 0*time.Second; i -= time.Second {
		buckets.Record(now.Add(i), 5)
	}

	if got, want := buckets.WindowAverage(now.Add(5*time.Second)), 5.; got != want {
		// we wrote a 5 every second, and we never wrote in the same second twice,
		// so the average _should_ be 5.
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}
}

func TestTimedFloat64BucketsHoles(t *testing.T) {
	now := time.Now()
	buckets := NewTimedFloat64Buckets(5*time.Second, granularity)

	for i := time.Duration(0); i < 5; i++ {
		buckets.Record(now.Add(i*time.Second), float64(i+1))
	}

	sum := 0.

	buckets.forEachBucket(now.Add(4*time.Second),
		func(_ time.Time, b float64) {
			sum += b
		})

	if got, want := sum, 15.; got != want {
		t.Errorf("Sum = %v, want: %v", got, want)
	}
	if got, want := buckets.WindowAverage(now.Add(4*time.Second)), 15./5; got != want {
		t.Errorf("WindowAverage = %v, want: %v", got, want)
	}
	// Now write at 9th second. Which means that seconds
	// 5[0], 6[1], 7[2] become 0.
	buckets.Record(now.Add(8*time.Second), 2.)
	// So now we have [3] = 2, [4] = 5 and sum should be 7.
	sum = 0.

	buckets.forEachBucket(now.Add(8*time.Second),
		func(_ time.Time, b float64) {
			sum += b
		})
	if got, want := sum, 7.; got != want {
		t.Errorf("Sum = %v, want: %v", got, want)
	}
}

func TestTimedFloat64BucketsWindowUpdate(t *testing.T) {
	startTime := time.Now()
	buckets := NewTimedFloat64Buckets(5*time.Second, granularity)

	// Fill the whole bucketing list with rollover.
	buckets.Record(startTime, 1)
	buckets.Record(startTime.Add(1*time.Second), 2)
	buckets.Record(startTime.Add(2*time.Second), 3)
	buckets.Record(startTime.Add(3*time.Second), 4)
	buckets.Record(startTime.Add(4*time.Second), 5)
	buckets.Record(startTime.Add(5*time.Second), 6)
	now := startTime.Add(5 * time.Second)

	sum := 0.
	buckets.forEachBucket(now, func(t time.Time, b float64) {
		sum += b
	})
	const wantInitial = 2. + 3 + 4 + 5 + 6
	if got, want := sum, wantInitial; got != want {
		t.Fatalf("Initial data set Sum = %v, want: %v", got, want)
	}
	if got, want := buckets.WindowAverage(now), wantInitial/5; got != want {
		t.Fatalf("Initial data set Sum = %v, want: %v", got, want)
	}

	// Increase window.
	buckets.ResizeWindow(10 * time.Second)
	if got, want := len(buckets.buckets), 10; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}
	if got, want := buckets.window, 10*time.Second; got != want {
		t.Fatalf("Resized bucket windows = %v, want: %v", got, want)
	}

	// Verify values were properly copied.
	sum = 0.
	buckets.forEachBucket(now, func(t time.Time, b float64) {
		sum += b
	})
	if got, want := sum, wantInitial; got != want {
		t.Fatalf("After first resize data set Sum = %v, want: %v", got, want)
	}
	// Note the average doesn't change, since we know we had at most 5 buckets.
	if got, want := buckets.WindowAverage(now), wantInitial/5; got != want {
		t.Errorf("Initial data set Sum = %v, want: %v", got, want)
	}

	// Add one more. Make sure all the data is preserved, since window is longer.
	now = now.Add(time.Second)
	buckets.Record(now, 7)
	const wantWithUpdate = wantInitial + 7
	sum = 0.
	buckets.forEachBucket(now, func(t time.Time, b float64) {
		sum += b
	})
	if got, want := sum, wantWithUpdate; got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}
	// Same here. We just have at most 6 recorded buckets.
	if got, want := buckets.WindowAverage(now), roundToNDigits(6, wantWithUpdate/6); got != want {
		t.Fatalf("Initial data set Sum = %v, want: %v", got, want)
	}

	// Now let's reduce window size.
	buckets.ResizeWindow(4 * time.Second)
	if got, want := len(buckets.buckets), 4; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}
	// Just last 4 buckets should have remained (so 2 oldest are expunged).
	const wantWithShrink = wantWithUpdate - 2 - 3
	sum = 0.
	buckets.forEachBucket(now, func(t time.Time, b float64) {
		sum += b
	})
	if got, want := sum, wantWithShrink; got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}
	if got, want := buckets.WindowAverage(now), wantWithShrink/4; got != want {
		t.Fatalf("Initial data set Sum = %v, want: %v", got, want)
	}

	// Verify idempotence.
	ob := &buckets.buckets
	buckets.ResizeWindow(4 * time.Second)
	if ob != &buckets.buckets {
		t.Error("The buckets have changed, though window didn't")
	}
}

func TestTimedFloat64BucketsWindowUpdate3sGranularity(t *testing.T) {
	const granularity = 3 * time.Second
	trunc1 := time.Now().Truncate(granularity)

	// So two buckets here (ceil(5/3)=ceil(1.6(6))=2).
	buckets := NewTimedFloat64Buckets(5*time.Second, granularity)
	if got, want := len(buckets.buckets), 2; got != want {
		t.Fatalf("Initial bucket count = %d, want: %d", got, want)
	}

	// Fill the whole bucketing list.
	buckets.Record(trunc1, 10)
	buckets.Record(trunc1.Add(1*time.Second), 2)
	buckets.Record(trunc1.Add(2*time.Second), 3)
	buckets.Record(trunc1.Add(3*time.Second), 4)
	buckets.Record(trunc1.Add(4*time.Second), 5)
	buckets.Record(trunc1.Add(5*time.Second), 6)
	buckets.Record(trunc1.Add(6*time.Second), 7) // This overrides the initial 15 (10+2+3)
	sum := 0.
	buckets.forEachBucket(trunc1.Add(6*time.Second), func(t time.Time, b float64) {
		sum += b
	})
	want := (4. + 5 + 6) + 7
	if got, want := sum, want; got != want {
		t.Fatalf("Initial data set Sum = %v, want: %v", got, want)
	}

	// Increase window.
	buckets.ResizeWindow(10 * time.Second)
	if got, want := len(buckets.buckets), 4; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}
	if got, want := buckets.window, 10*time.Second; got != want {
		t.Fatalf("Resized bucket windows = %v, want: %v", got, want)
	}

	// Verify values were properly copied.
	sum = 0
	buckets.forEachBucket(trunc1.Add(6*time.Second), func(t time.Time, b float64) {
		sum += b
	})
	if got, want := sum, want; got != want {
		t.Fatalf("After first resize data set Sum = %v, want: %v", got, want)
	}

	// Add one more. Make sure all the data is preserved, since window is longer.
	buckets.Record(trunc1.Add(9*time.Second+300*time.Millisecond), 42)
	sum = 0
	buckets.forEachBucket(trunc1.Add(9*time.Second), func(t time.Time, b float64) {
		sum += b
	})
	want += 42
	if got, want := sum, want; got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}

	// Now let's reduce window size.
	buckets.ResizeWindow(4 * time.Second)

	sum = 0
	if got, want := len(buckets.buckets), 2; got != want {
		t.Fatalf("Resized bucket count = %d, want: %d", got, want)
	}

	// Just last 4 buckets should have remained.
	sum = 0.
	want = 42 + 7 // we drop oldest bucket and the one not yet utilized)
	buckets.forEachBucket(trunc1.Add(9*time.Second), func(t time.Time, b float64) {
		sum += b
	})
	if got, want := sum, want; got != want {
		t.Fatalf("Updated data set Sum = %v, want: %v", got, want)
	}

	// Verify idempotence.
	ob := &buckets.buckets
	buckets.ResizeWindow(4 * time.Second)
	if ob != &buckets.buckets {
		t.Error("The buckets have changed, though window didn't")
	}
}

func TestTimedFloat64BucketsWindowUpdateNoOp(t *testing.T) {
	startTime := time.Now().Add(-time.Minute)
	buckets := NewTimedFloat64Buckets(5*time.Second, granularity)
	buckets.Record(startTime, 19.82)
	if got, want := buckets.firstWrite, buckets.lastWrite; !got.Equal(want) {
		t.Errorf("FirstWrite = %v, want: %v", got, want)
	}
	buckets.ResizeWindow(10 * time.Second)

	if got, want := buckets.firstWrite, (time.Time{}); !got.Equal(want) {
		t.Errorf("FirstWrite after update = %v, want: %v", got, want)
	}
}
func BenchmarkWindowAverage(b *testing.B) {
	// Window lengths in secs.
	for _, wl := range []int{30, 60, 120, 240, 600} {
		b.Run(fmt.Sprintf("%v-win-len", wl), func(b *testing.B) {
			tn := time.Now().Truncate(time.Second) // To simplify everything.
			buckets := NewTimedFloat64Buckets(time.Duration(wl)*time.Second,
				time.Second /*granularity*/)
			// Populate with some random data.
			for i := 0; i < wl; i++ {
				buckets.Record(tn.Add(time.Duration(i)*time.Second), rand.Float64()*100)
			}
			for i := 0; i < b.N; i++ {
				buckets.WindowAverage(tn.Add(time.Duration(wl) * time.Second))
			}
		})
	}
}

func TestRoundToNDigits(t *testing.T) {
	if got, want := roundToNDigits(6, 3.6e-17), 0.; got != want {
		t.Errorf("Rounding = %v, want: %v", got, want)
	}
	if got, want := roundToNDigits(3, 0.0004), 0.; got != want {
		t.Errorf("Rounding = %v, want: %v", got, want)
	}
	if got, want := roundToNDigits(3, 1.2345), 1.234; got != want {
		t.Errorf("Rounding = %v, want: %v", got, want)
	}
	if got, want := roundToNDigits(4, 1.2345), 1.2345; got != want {
		t.Errorf("Rounding = %v, want: %v", got, want)
	}
	if got, want := roundToNDigits(6, 12345), 12345.; got != want {
		t.Errorf("Rounding = %v, want: %v", got, want)
	}

}

func (t *TimedFloat64Buckets) forEachBucket(now time.Time, acc func(time time.Time, bucket float64)) {
	now = now.Truncate(t.granularity)
	t.bucketsMutex.RLock()
	defer t.bucketsMutex.RUnlock()

	// So number of buckets we can process is len(buckets)-(now-lastWrite)/granularity.
	// Since empty check above failed, we know this is at least 1 bucket.
	numBuckets := len(t.buckets) - int(now.Sub(t.lastWrite)/t.granularity)
	bucketTime := t.lastWrite // Always aligned with granularity.
	si := t.timeToIndex(bucketTime)
	for i := 0; i < numBuckets; i++ {
		tIdx := si % len(t.buckets)
		acc(bucketTime, t.buckets[tIdx])
		si--
		bucketTime = bucketTime.Add(-t.granularity)
	}
}
