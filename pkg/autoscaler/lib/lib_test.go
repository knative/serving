package lib

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"
)

func TestAutoscaler_NoData_NoAutoscale(t *testing.T) {
	a := NewAutoscaler(10.0)
	a.expectScale(t, time.Now(), 0, false)
}

func TestAutoscaler_StableMode_NoChange(t *testing.T) {
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
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
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
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
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
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
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         1,
		})
	a.expectScale(t, now, 1, true)
}

func TestAutoscaler_StableModeLowTraffic_ScaleToOne(t *testing.T) {
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
		time.Now(),
		linearSeries{
			startConcurrency: 0,
			endConcurrency:   0,
			durationSeconds:  60,
			podCount:         2,
		})
	a.expectScale(t, now, 1, true)
}

func TestAutoscaler_PanicMode_DoublePodCount(t *testing.T) {
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         10,
		})
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 20,
			endConcurrency:   20,
			durationSeconds:  6,
			podCount:         10,
		})
	a.expectScale(t, now, 20, true)
}

func TestAutoscaler_PanicModeExponential_TrackAndStablize(t *testing.T) {
	a := NewAutoscaler(1.0)
	now := a.recordLinearSeries(
		time.Now(),
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         1,
		})
	a.expectScale(t, now, 6, true)
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         6,
		})
	a.expectScale(t, now, 36, true)
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         36,
		})
	a.expectScale(t, now, 216, true)
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   10,
			durationSeconds:  6,
			podCount:         216,
		})
	a.expectScale(t, now, 1296, true)
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   1,
			durationSeconds:  6,
			podCount:         1296,
		})
	a.expectScale(t, now, 1296, true)
}

func TestAutoscaler_PanicThenUnPanic_ScaleDown(t *testing.T) {
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
		time.Now(),
		linearSeries{
			startConcurrency: 10,
			endConcurrency:   10,
			durationSeconds:  60,
			podCount:         10,
		})
	a.expectScale(t, now, 10, true)
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 100,
			endConcurrency:   100,
			durationSeconds:  6,
			podCount:         10,
		})
	a.expectScale(t, now, 100, true)
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   1,
			durationSeconds:  30,
			podCount:         100,
		})
	a.expectScale(t, now, 100, true)
	now = a.recordLinearSeries(
		now,
		linearSeries{
			startConcurrency: 1,
			endConcurrency:   1,
			durationSeconds:  31,
			podCount:         100,
		})
	a.expectScale(t, now, 10, true)
}

func TestAutoscaler_Stats_TrimAfterStableWindow(t *testing.T) {
	a := NewAutoscaler(10.0)
	now := a.recordLinearSeries(
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

type linearSeries struct {
	startConcurrency int
	endConcurrency   int
	durationSeconds  int
	podCount         int
}

func (a *Autoscaler) recordLinearSeries(now time.Time, s linearSeries) time.Time {
	points := make([]int32, 0)
	for i := 1; i <= s.durationSeconds; i++ {
		points = append(points, int32(float64(s.startConcurrency)+float64(s.endConcurrency-s.startConcurrency)*(float64(i)/float64(s.durationSeconds))))
	}
	log.Printf("Recording points: %v.", points)
	for _, point := range points {
		t := now
		now = now.Add(time.Second)
		for j := 1; j <= s.podCount; j++ {
			t = t.Add(time.Millisecond)
			stat := types.Stat{
				PodName:            fmt.Sprintf("pod-%v", j),
				ConcurrentRequests: point,
			}
			a.Record(stat, t)
		}
	}
	return now
}

func (a *Autoscaler) expectScale(t *testing.T, now time.Time, expectScale int32, expectOk bool) {
	scale, ok := a.Scale(now)
	if ok != expectOk {
		t.Errorf("Unexpected autoscale decison. Expected %v. Got %v.", expectOk, ok)
	}
	if scale != expectScale {
		t.Errorf("Unexpected scale. Expected %v. Got %v.", expectScale, scale)
	}
}
