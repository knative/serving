package scaling

import (
	"context"
	"testing"
	"time"

	logtesting "knative.dev/pkg/logging/testing"
)

type fakePodCounterEC struct {
	ready     int
	notReady  int
	effective int
}

func (f fakePodCounterEC) ReadyCount() (int, error)             { return f.ready, nil }
func (f fakePodCounterEC) NotReadyCount() (int, error)          { return f.notReady, nil }
func (f fakePodCounterEC) EffectiveCapacityCount() (int, error) { return f.effective, nil }

func TestHeadroomAppliesWhenReachableAndObservedLoad(t *testing.T) {
	pc := &fakePodCounterEC{ready: 0, effective: 0}
	mc := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      10,
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   100,
		ScaleBuffer:      5,
		Reachable:        true,
		RevisionReady:    false, // not ready yet
		StableWindow:     stableWindow,
	}
	as := New(context.Background(), testNamespace, testRevision, mc, pc, spec)

	now := time.Now()
	// Non-zero observed load should apply headroom even if not RevisionReady
	mc.SetStableAndPanicConcurrency(10, 10)
	expectScale(t, as, now, ScaleResult{ScaleValid: true, DesiredPodCount: 2}) // ceil((10+5)/10)=2
}

func TestPanicThresholdUsesReadyDenominator(t *testing.T) {
	// effective>ready; desiredPanic based on observed; denominator should be ready, not effective
	pc := &fakePodCounterEC{ready: 1, effective: 10}
	mc := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      1, // so desiredPanic = observedPanic
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   2, // require desired/den >=2 to panic
		ScaleBuffer:      0,
		Reachable:        true,
		RevisionReady:    true,
		StableWindow:     stableWindow,
	}
	as := New(context.Background(), testNamespace, testRevision, mc, pc, spec)

	now := time.Now()
	// observedPanic=3 -> desiredPanic=3; denominator should be ready=1 -> 3/1=3 >=2 => panic
	mc.SetStableAndPanicConcurrency(0, 3)
	res := as.Scale(logtesting.TestLogger(t), now)
	if !res.ScaleValid {
		t.Fatalf("scale invalid")
	}
	if as.(*autoscaler).panicTime.IsZero() {
		t.Fatalf("expected panic to trigger with ready denominator")
	}
}
