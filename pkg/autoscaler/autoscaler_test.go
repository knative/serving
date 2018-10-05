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

package autoscaler

import (
	"fmt"
	"testing"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"

	. "github.com/knative/pkg/logging/testing"
	"go.uber.org/zap"
)

func TestAutoscaler_NoData_NoAutoscale(t *testing.T) {
	a := newTestAutoscaler(10.0)
	a.expectScale(t, time.Now(), 0, false)
}

func TestAutoscaler_NoDataAtZero_NoAutoscale(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 0,
			endConcurrency:   0,
			durationSeconds:  300, // 5 minutes
			podCount:         1,
		})

	a.expectScale(t, now, 0, true)
	now = now.Add(2 * time.Minute)
	a.expectScale(t, now, 0, false) // do nothing
}

func TestAutoscaler_StableMode_NoChange(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         10,
		})
	a.expectScale(t, now, 10, true)
}

func TestAutoscaler_StableMode_SlowIncrease(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   20,
			durationSeconds:  60,
			podCount:         10,
		})
	a.expectScale(t, now, 15, true)
}

func TestAutoscaler_StableMode_SlowDecrease(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 20,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         10,
		})
	a.expectScale(t, now, 15, true)
}

func TestAutoscaler_StableModeLowPodCount_NoChange(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         1,
		})
	a.expectScale(t, now, 1, true)
}

func TestAutoscaler_StableModeNoTraffic_ScaleToOne(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 0,
			endConcurrency:   0,
			durationSeconds:  60,
			podCount:         2,
		})
	a.expectScale(t, now, 1, true)
}

func TestAutoscaler_StableModeNoTraffic_ScaleToZero(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   1,
			durationSeconds:  60,
			podCount:         1,
		})

	a.expectScale(t, now, 1, true)
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 0,
			endConcurrency:   0,
			durationSeconds:  300, // 5 minutes
			podCount:         1,
		})
	a.expectScale(t, now, 0, true)

	// Should not scale to zero again if there is no more traffic.
	// Note: scale of 1 will be ignored since the autoscaler is not responsible for scaling from 0.
	a.expectScale(t, now, 0, true)
}

func TestAutoscaler_PanicMode_DoublePodCount(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         10,
		})
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 20,
			endConcurrency:   20,
			durationSeconds:  6,
			podCount:         10,
		})
	a.expectScale(t, now, 20, true)
}

// QPS is increasing exponentially. Each scaling event bring concurrency
// back to the target level (1.0) but then traffic continues to increase.
// At 1296 QPS traffic stablizes.
func TestAutoscaler_PanicModeExponential_TrackAndStablize(t *testing.T) {
	a := newTestAutoscaler(1.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         1,
		})
	a.expectScale(t, now, 6, true)
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         6,
		})
	a.expectScale(t, now, 36, true)
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         36,
		})
	a.expectScale(t, now, 216, true)
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         216,
		})
	a.expectScale(t, now, 1296, true)
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   1, // achieved desired concurrency
			durationSeconds:  6,
			podCount:         1296,
		})
	a.expectScale(t, now, 1296, true)
}

func TestAutoscaler_PanicThenUnPanic_ScaleDown(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         10,
		})
	a.expectScale(t, now, 10, true)
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 100,
			endConcurrency:   100,
			durationSeconds:  6,
			podCount:         10,
		})
	a.expectScale(t, now, 100, true)
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 1, // traffic drops off
			endConcurrency:   1,
			durationSeconds:  30,
			podCount:         100,
		})
	a.expectScale(t, now, 100, true) // still in panic mode--no decrease
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   1,
			durationSeconds:  31,
			podCount:         100,
		})
	a.expectScale(t, now, 10, true) // back to stable mode
}

func TestAutoscaler_NoScaleOnLessThanOnePod(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  10, // 10 seconds of 2 pods
			podCount:         2,
		})
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  50, // 50 seconds of 0 pods (lameducked)
			podCount:         2,
			lameduck:         true,
		})
	a.expectScale(t, now, 0, false)
}

func TestAutoscaler_LameDuckDoesNotCount(t *testing.T) {
	a := newTestAutoscaler(10.0)
	start := time.Now()
	end := a.recordLinearSeries(
		t,
		start,
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60, // 1 pod active
			podCount:         1,
			podIdOffset:      0,
		})
	a.recordLinearSeries(
		t,
		start,
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60, // 1 pod lameducked
			podCount:         1,
			podIdOffset:      1,
			lameduck:         true,
		})
	a.expectScale(t, end, 1, true) // 2 pods reporting metrics but one doesn't count
}

func TestAutoscaler_LameDucksAreAmortized(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  30,
			podCount:         10,
		})
	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  31, // one extra second because float and ceiling
			podCount:         10,
			lameduck:         true,
		})
	a.expectScale(t, now, 5, true) // 10 pods lameducked half the time count for 5
}

func TestAutoscaler_Activator_CausesInstantScale(t *testing.T) {
	a := newTestAutoscaler(10.0)

	now := time.Now()
	now = a.recordMetric(t, Stat{
		Time:                      &now,
		PodName:                   ActivatorPodName,
		RequestCount:              0,
		AverageConcurrentRequests: 100.0,
	})

	a.expectScale(t, now, 10, true)
}

func TestAutoscaler_Activator_IsIgnored(t *testing.T) {
	a := newTestAutoscaler(10.0)

	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  30,
			podCount:         10,
		})

	a.expectScale(t, now, 10, true)

	now = a.recordMetric(t, Stat{
		Time:                      &now,
		PodName:                   ActivatorPodName,
		RequestCount:              0,
		AverageConcurrentRequests: 1000.0,
	})

	// Scale should not change as the activator metric should
	// be ignored
	a.expectScale(t, now, 10, true)
}

// Autoscaler should drop data after 60 seconds.
func TestAutoscaler_Stats_TrimAfterStableWindow(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         1,
		})
	a.expectScale(t, now, 1, true)
	if len(a.stats) != 60 {
		t.Errorf("Unexpected stat count. Expected 60. Got %v.", len(a.stats))
	}
	now = now.Add(time.Minute)
	a.expectScale(t, now, 0, false)
	if len(a.stats) != 0 {
		t.Errorf("Unexpected stat count. Expected 0. Got %v.", len(a.stats))
	}
}

func TestAutoscaler_Stats_DenyNoTime(t *testing.T) {
	a := newTestAutoscaler(10.0)
	stat := Stat{
		Time:                      nil,
		PodName:                   "pod-1",
		AverageConcurrentRequests: 1.0,
		RequestCount:              5,
	}
	a.Record(TestContextWithLogger(t), stat)

	if len(a.stats) != 0 {
		t.Errorf("Unexpected stat count. Expected 0. Got %v.", len(a.stats))
	}
	a.expectScale(t, time.Now(), 0, false)
}

func TestAutoscaler_RateLimit_ScaleUp(t *testing.T) {
	a := newTestAutoscaler(10.0)
	now := a.recordLinearSeries(
		t,
		time.Now(),
		linearSeries{
			startConcurrency: 1000,
			endConcurrency:   1000,
			durationSeconds:  1,
			podCount:         1,
		})

	// Need 100 pods but only scale x10
	a.expectScale(t, now, 10, true)

	now = a.recordLinearSeries(
		t,
		now,
		linearSeries{
			startConcurrency: 1000,
			endConcurrency:   1000,
			durationSeconds:  1,
			podCount:         10,
		})

	// Scale x10 again
	a.expectScale(t, now, 100, true)
}

type linearSeries struct {
	startConcurrency int
	endConcurrency   int
	durationSeconds  int
	podCount         int
	podIdOffset      int
	lameduck         bool
}

type mockReporter struct{}

func (r *mockReporter) Report(m Measurement, v float64) error {
	return nil
}

func newTestAutoscaler(containerConcurrency int) *Autoscaler {
	stableWindow := 60 * time.Second
	panicWindow := 6 * time.Second
	scaleToZeroIdlePeriod := 4*time.Minute + 30*time.Second
	scaleToZeroGracePeriod := 30 * time.Second
	config := &Config{
		ContainerConcurrencyTargetPercentage: 1.0, // targetting 100% makes the test easier to read
		ContainerConcurrencyTargetDefault:    10.0,
		MaxScaleUpRate:                       10.0,
		StableWindow:                         stableWindow,
		PanicWindow:                          panicWindow,
		ScaleToZeroThreshold:                 scaleToZeroIdlePeriod + scaleToZeroGracePeriod,
		ScaleToZeroIdlePeriod:                scaleToZeroIdlePeriod,
		ScaleToZeroGracePeriod:               scaleToZeroGracePeriod,
	}

	dynConfig := &DynamicConfig{
		config: config,
		logger: zap.NewNop().Sugar(),
	}
	return New(dynConfig, v1alpha1.RevisionContainerConcurrencyType(containerConcurrency), &mockReporter{})
}

// Record a data point every second, for every pod, for duration of the
// linear series, on the line from start to end concurrency.
func (a *Autoscaler) recordLinearSeries(test *testing.T, now time.Time, s linearSeries) time.Time {
	points := make([]int32, 0)
	for i := 1; i <= s.durationSeconds; i++ {
		points = append(points, int32(float64(s.startConcurrency)+float64(s.endConcurrency-s.startConcurrency)*(float64(i)/float64(s.durationSeconds))))
	}
	test.Logf("Recording points: %v.", points)
	for _, point := range points {
		t := now
		now = now.Add(time.Second)
		for j := 1; j <= s.podCount; j++ {
			t = t.Add(time.Millisecond)
			requestCount := 0
			if point > 0 {
				requestCount = 1
			}
			stat := Stat{
				Time:                      &t,
				PodName:                   fmt.Sprintf("pod-%v", j+s.podIdOffset),
				AverageConcurrentRequests: float64(point),
				RequestCount:              int32(requestCount),
				LameDuck:                  s.lameduck,
			}
			a.Record(TestContextWithLogger(test), stat)
		}
	}
	return now
}

// Record a single datapoint
func (a *Autoscaler) recordMetric(test *testing.T, stat Stat) time.Time {
	a.Record(TestContextWithLogger(test), stat)
	return *stat.Time
}

func (a *Autoscaler) expectScale(t *testing.T, now time.Time, expectScale int32, expectOk bool) {
	t.Helper()
	scale, ok := a.Scale(TestContextWithLogger(t), now)
	if ok != expectOk {
		t.Errorf("Unexpected autoscale decision. Expected %v. Got %v.", expectOk, ok)
	}
	if scale != expectScale {
		t.Errorf("Unexpected scale. Expected %v. Got %v.", expectScale, scale)
	}
}
