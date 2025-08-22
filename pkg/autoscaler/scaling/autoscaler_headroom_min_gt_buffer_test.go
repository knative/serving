package scaling

import (
	"context"
	logtesting "knative.dev/pkg/logging/testing"
	"testing"
	"time"
)

func TestHeadroom_MinGreaterThanBuffer_Threshold(t *testing.T) {
	pc := &fakePodCounter{readyCount: 20}
	metrics := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      1,
		MaxScaleDownRate: 100,
		MaxScaleUpRate:   100,
		PanicThreshold:   1000,
		ScaleBuffer:      10,
		Reachable:        true,
		RevisionReady:    true,
		StableWindow:     stableWindow,
		MinScale:         20,
	}
	as := New(context.Background(), testNamespace, testRevision, metrics, pc, spec)
	now := time.Now()

	// in_use <= threshold (min*target - B) = 20-10=10 -> stay at 20
	metrics.SetStableAndPanicConcurrency(5, 5)
	_ = as.Scale(logtesting.TestLogger(t), now)
	expectScale(t, as, now.Add(10*time.Millisecond), ScaleResult{ScaleValid: true, DesiredPodCount: 20})

	metrics.SetStableAndPanicConcurrency(10, 10)
	_ = as.Scale(logtesting.TestLogger(t), now)
	expectScale(t, as, now.Add(20*time.Millisecond), ScaleResult{ScaleValid: true, DesiredPodCount: 20})

	// in_use > threshold -> desired = max(min, ceil((in_use + B)/target))
	metrics.SetStableAndPanicConcurrency(11, 11)
	_ = as.Scale(logtesting.TestLogger(t), now)
	expectScale(t, as, now.Add(30*time.Millisecond), ScaleResult{ScaleValid: true, DesiredPodCount: 21})
}
