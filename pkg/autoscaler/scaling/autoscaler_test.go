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

package scaling

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.opencensus.io/resource"

	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"

	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/autoscaler/metrics"
	smetrics "knative.dev/serving/pkg/metrics"
	"knative.dev/serving/pkg/resources"

	. "knative.dev/pkg/logging/testing"
	_ "knative.dev/pkg/metrics/testing"
)

const (
	stableWindow      = 60 * time.Second
	targetUtilization = 0.75
	activatorCapacity = 150

	testRevision  = "a-revision-to-scale"
	testNamespace = "in-this-namespace"
)

var wantResource = &resource.Resource{
	Type: "knative_revision",
	Labels: map[string]string{
		metricskey.LabelConfigurationName: "testConfig",
		metricskey.LabelNamespaceName:     testNamespace,
		metricskey.LabelRevisionName:      testRevision,
		metricskey.LabelServiceName:       "testSvc",
	},
}

type fakePodCounter struct {
	resources.EndpointsCounter
	readyCount int
	err        error
}

func (fpc fakePodCounter) ReadyCount() (int, error) {
	return fpc.readyCount, fpc.err
}

func TestAutoscalerScaleDownDelay(t *testing.T) {
	pc := &fakePodCounter{}
	metrics := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      10,
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   100,
		ScaleDownDelay:   5 * time.Minute,
	}
	as := New(TestContextWithLogger(t), testNamespace, testRevision, metrics, pc, spec)

	now := time.Time{}

	t.Run("simple", func(t *testing.T) {
		// scale up.
		metrics.SetStableAndPanicConcurrency(40, 40)
		expectScale(t, as, now.Add(2*time.Second), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4,
			NumActivators:   2,
		})
		// five minutes pass at reduced concurrency - should not scale down (less than delay).
		metrics.SetStableAndPanicConcurrency(0, 0)
		expectScale(t, as, now.Add(5*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4,
			NumActivators:   2,
		})
		// five minutes and 2 seconds pass at reduced concurrency - now we scale down.
		expectScale(t, as, now.Add(5*time.Minute+2*time.Second), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 0,
			NumActivators:   2,
		})
	})

	t.Run("gradual", func(t *testing.T) {
		metrics.SetStableAndPanicConcurrency(40, 40)
		expectScale(t, as, now.Add(9*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4,
			NumActivators:   2,
		})
		metrics.SetStableAndPanicConcurrency(30, 30)
		expectScale(t, as, now.Add(10*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4, // 4 still dominates
			NumActivators:   2,
		})
		metrics.SetStableAndPanicConcurrency(0, 0)
		expectScale(t, as, now.Add(11*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4, // still at 4
			NumActivators:   2,
		})
		expectScale(t, as, now.Add(14*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 3, // 4 scrolls out, drop to 3
			NumActivators:   2,
		})
		expectScale(t, as, now.Add(15*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 0, // everything scrolled out, drop to 0
			NumActivators:   2,
		})
	})
}

func TestAutoscalerScaleDownDelayZero(t *testing.T) {
	pc := &fakePodCounter{}
	metrics := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      10,
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   100,
		ScaleDownDelay:   0,
	}
	as := New(TestContextWithLogger(t), testNamespace, testRevision, metrics, pc, spec)

	now := time.Time{}

	metrics.SetStableAndPanicConcurrency(40, 40)
	expectScale(t, as, now, ScaleResult{
		ScaleValid:      true,
		DesiredPodCount: 4,
		NumActivators:   2,
	})
	// With zero delay we should immediately scale down (this is not the case
	// with a 1-element time window delay).
	metrics.SetStableAndPanicConcurrency(20, 20)
	expectScale(t, as, now.Add(500*time.Millisecond), ScaleResult{
		ScaleValid:      true,
		DesiredPodCount: 2,
		NumActivators:   2,
	})
}

func TestAutoscalerNoDataNoAutoscale(t *testing.T) {
	defer reset()
	metrics := &metricClient{
		ErrF: func(types.NamespacedName, time.Time) error {
			return errors.New("no metrics")
		},
	}

	a := newTestAutoscalerNoPC(10, 100, metrics)
	expectScale(t, a, time.Now(), ScaleResult{0, 0, MinActivators, false})
}

func expectedEBC(totCap, targetBC, recordedConcurrency, numPods float64) int32 {
	ebcF := totCap/targetUtilization*numPods - targetBC - recordedConcurrency
	// We need to floor for negative values.
	return int32(math.Floor(ebcF))
}

func expectedNA(a *autoscaler, numP float64) int32 {
	return int32(math.Max(MinActivators,
		math.Ceil(
			(a.deciderSpec.TotalValue*numP+a.deciderSpec.TargetBurstCapacity)/a.deciderSpec.ActivatorCapacity)))
}

func TestAutoscalerStartMetrics(t *testing.T) {
	defer reset()
	metrics := &metricClient{StableConcurrency: 50.0, PanicConcurrency: 50.0}
	newTestAutoscalerWithScalingMetric(10, 100, metrics,
		"concurrency", true /*startInPanic*/)
	metricstest.AssertMetric(t, metricstest.IntMetric(panicM.Name(), 1, nil).WithResource(wantResource))
}

func TestAutoscalerMetrics(t *testing.T) {
	defer reset()

	metrics := &metricClient{StableConcurrency: 50.0, PanicConcurrency: 50.0}
	a := newTestAutoscalerNoPC(10, 100, metrics)
	// Non-panic created autoscaler.
	metricstest.AssertMetric(t, metricstest.IntMetric(panicM.Name(), 0, nil).WithResource(wantResource))
	ebc := expectedEBC(10, 100, 50, 1)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{5, ebc, na, true})
	spec := a.currentSpec()

	wantMetrics := []metricstest.Metric{
		metricstest.FloatMetric(stableRequestConcurrencyM.Name(), 50, nil).WithResource(wantResource),
		metricstest.FloatMetric(panicRequestConcurrencyM.Name(), 50, nil).WithResource(wantResource),
		metricstest.IntMetric(desiredPodCountM.Name(), 5, nil).WithResource(wantResource),
		metricstest.FloatMetric(targetRequestConcurrencyM.Name(), spec.TargetValue, nil).WithResource(wantResource),
		metricstest.FloatMetric(excessBurstCapacityM.Name(), float64(ebc), nil).WithResource(wantResource),
		metricstest.IntMetric(panicM.Name(), 1, nil).WithResource(wantResource),
	}
	metricstest.AssertMetric(t, wantMetrics...)
}

func TestAutoscalerMetricsWithRPS(t *testing.T) {
	defer reset()
	metrics := &metricClient{PanicRPS: 99.0, StableRPS: 100}
	a, _ := newTestAutoscalerWithScalingMetric(10, 100, metrics, "rps", false /*startInPanic*/)
	ebc := expectedEBC(10, 100, 99, 1)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{10, ebc, na, true})
	spec := a.currentSpec()

	expectScale(t, a, time.Now().Add(61*time.Second), ScaleResult{10, ebc, na, true})
	wantMetrics := []metricstest.Metric{
		metricstest.FloatMetric(stableRPSM.Name(), 100, nil).WithResource(wantResource),
		metricstest.FloatMetric(panicRPSM.Name(), 100, nil).WithResource(wantResource),
		metricstest.IntMetric(desiredPodCountM.Name(), 10, nil).WithResource(wantResource),
		metricstest.FloatMetric(targetRPSM.Name(), spec.TargetValue, nil).WithResource(wantResource),
		metricstest.FloatMetric(excessBurstCapacityM.Name(), float64(ebc), nil).WithResource(wantResource),
		metricstest.IntMetric(panicM.Name(), 1, nil).WithResource(wantResource),
	}
	metricstest.AssertMetric(t, wantMetrics...)
}

func TestAutoscalerStableModeIncreaseWithConcurrencyDefault(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 50.0, PanicConcurrency: 10}
	a := newTestAutoscalerNoPC(10, 101, metrics)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{5, expectedEBC(10, 101, 10, 1), na, true})

	metrics.StableConcurrency = 100
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 101, 10, 1), na, true})
}

func TestAutoscalerStableModeIncreaseWithRPS(t *testing.T) {
	metrics := &metricClient{StableRPS: 50.0, PanicRPS: 50}
	a, _ := newTestAutoscalerWithScalingMetric(10, 101, metrics, "rps", false /*startInPanic*/)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{5, expectedEBC(10, 101, 50, 1), na, true})

	metrics.StableRPS = 100
	metrics.PanicRPS = 99
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 101, 99, 1), na, true})
}

func TestAutoscalerUnpanicAfterSlowIncrease(t *testing.T) {
	// Do initial jump from 10 to 25 pods.
	metrics := &metricClient{StableConcurrency: 11, PanicConcurrency: 25}
	a, pc := newTestAutoscaler(1, 98, metrics)
	pc.readyCount = 10

	na := expectedNA(a, 10)
	start := time.Now()
	tm := start
	expectScale(t, a, tm, ScaleResult{25, expectedEBC(1, 98, 25, 10), na, true})
	if a.panicTime != tm {
		t.Errorf("PanicTime = %v, want: %v", a.panicTime, tm)
	}
	// Now the half of the stable window has passed, and we've been adding 1 pod per cycle.
	// For window of 60s, that's +15 pods.
	pc.readyCount = 25 + 15

	metrics.SetStableAndPanicConcurrency(30, 41)
	tm = tm.Add(stableWindow / 2)

	na = expectedNA(a, 40)
	expectScale(t, a, tm, ScaleResult{41, expectedEBC(1, 98, 41, 40), na, true})
	if a.panicTime != start {
		t.Error("Panic Time should not have moved")
	}

	// Now at the end. Panic must end. And +30 pods.
	pc.readyCount = 25 + 30
	metrics.SetStableAndPanicConcurrency(50, 56)
	tm = tm.Add(stableWindow/2 + tickInterval)

	na = expectedNA(a, 55)
	expectScale(t, a, tm, ScaleResult{50 /* no longer in panic*/, expectedEBC(1, 98, 56, 55), na, true})
	if !a.panicTime.IsZero() {
		t.Errorf("PanicTime = %v, want: 0", a.panicTime)
	}
}

func TestAutoscalerExtendPanicWindow(t *testing.T) {
	// Do initial jump from 10 to 25 pods.
	metrics := &metricClient{StableConcurrency: 11, PanicConcurrency: 25}
	a, pc := newTestAutoscaler(1, 98, metrics)
	pc.readyCount = 10

	na := expectedNA(a, 10)
	start := time.Now()
	tm := start
	expectScale(t, a, tm, ScaleResult{25, expectedEBC(1, 98, 25, 10), na, true})
	if a.panicTime != tm {
		t.Errorf("PanicTime = %v, want: %v", a.panicTime, tm)
	}
	// Now the half of the stable window has passed, and we're still surging.
	pc.readyCount = 25 + 15

	metrics.SetStableAndPanicConcurrency(30, 80)
	tm = tm.Add(stableWindow / 2)

	na = expectedNA(a, 40)
	expectScale(t, a, tm, ScaleResult{80, expectedEBC(1, 98, 80, 40), na, true})
	if a.panicTime != tm {
		t.Errorf("PanicTime = %v, want: %v", a.panicTime, tm)
	}
}

func TestAutoscalerStableModeDecrease(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 100.0, PanicConcurrency: 100}
	a, pc := newTestAutoscaler(10, 98, metrics)
	pc.readyCount = 8
	na := expectedNA(a, 8)
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 98, 100, 8), na, true})

	metrics.SetStableAndPanicConcurrency(50, 50)
	expectScale(t, a, time.Now(), ScaleResult{5, expectedEBC(10, 98, 50, 8), na, true})
}

func TestAutoscalerStableModeNoTrafficScaleToZero(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1, PanicConcurrency: 0}
	a := newTestAutoscalerNoPC(10, 75, metrics)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{1, expectedEBC(10, 75, 0, 1), na, true})

	metrics.StableConcurrency = 0.0
	expectScale(t, a, time.Now(), ScaleResult{0, expectedEBC(10, 75, 0, 1), na, true})
}

// QPS is increasing exponentially. Each scaling event bring concurrency
// back to the target level (1.0) but then traffic continues to increase.
// At 1296 QPS traffic stabilizes.
func TestAutoscalerPanicModeExponentialTrackAndStabilize(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 6, PanicConcurrency: 6}
	a, pc := newTestAutoscaler(1, 101, metrics)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{6, expectedEBC(1, 101, 6, 1), na, true})

	tm := time.Now()
	pc.readyCount = 6
	na = expectedNA(a, 6)
	metrics.SetStableAndPanicConcurrency(36, 36)
	expectScale(t, a, tm, ScaleResult{36, expectedEBC(1, 101, 36, 6), na, true})
	if got, want := a.panicTime, tm; got != tm {
		t.Errorf("PanicTime = %v, want: %v", got, want)
	}

	pc.readyCount = 36
	na = expectedNA(a, 36)
	metrics.SetStableAndPanicConcurrency(216, 216)
	tm = tm.Add(time.Second)
	expectScale(t, a, tm, ScaleResult{216, expectedEBC(1, 101, 216, 36), na, true})
	if got, want := a.panicTime, tm; got != tm {
		t.Errorf("PanicTime = %v, want: %v", got, want)
	}

	pc.readyCount = 216
	na = expectedNA(a, 216)
	metrics.SetStableAndPanicConcurrency(1296, 1296)
	expectScale(t, a, tm, ScaleResult{1296, expectedEBC(1, 101, 1296, 216), na, true})
	if got, want := a.panicTime, tm; got != tm {
		t.Errorf("PanicTime = %v, want: %v", got, want)
	}

	pc.readyCount = 1296
	na = expectedNA(a, 1296)
	tm = tm.Add(time.Second)
	expectScale(t, a, tm, ScaleResult{1296, expectedEBC(1, 101, 1296, 1296), na, true})
}

func TestAutoscalerScale(t *testing.T) {
	tests := []struct {
		label       string
		as          *autoscaler
		prepFunc    func(as *autoscaler)
		baseScale   int
		wantScale   int32
		wantEBC     int32
		wantInvalid bool
	}{{
		label:     "AutoscalerNoDataAtZeroNoAutoscale",
		as:        newTestAutoscalerNoPC(10, 100, &metricClient{}),
		baseScale: 1,
		wantScale: 0,
		wantEBC:   expectedEBC(10, 100, 0, 1),
	}, {
		label:     "AutoscalerNoDataAtZeroNoAutoscaleWithExplicitEPs",
		as:        newTestAutoscalerNoPC(10, 100, &metricClient{}),
		baseScale: 1,
		wantScale: 0,
		wantEBC:   expectedEBC(10, 100, 0, 1),
	}, {
		label:     "AutoscalerStableModeUnlimitedTBC",
		as:        newTestAutoscalerNoPC(181, -1, &metricClient{StableConcurrency: 21.0, PanicConcurrency: 26}),
		baseScale: 1,
		wantScale: 1,
		wantEBC:   -1,
	}, {
		label:     "Autoscaler0TBC",
		as:        newTestAutoscalerNoPC(10, 0, &metricClient{StableConcurrency: 50.0, PanicConcurrency: 49}),
		baseScale: 1,
		wantScale: 5,
		wantEBC:   0,
	}, {
		label:     "AutoscalerStableModeNoChange",
		as:        newTestAutoscalerNoPC(10, 100, &metricClient{StableConcurrency: 50.0, PanicConcurrency: 50}),
		baseScale: 1,
		wantScale: 5,
		wantEBC:   expectedEBC(10, 100, 50, 1),
	}, {
		label: "AutoscalerPanicStableLargerThanPanic",
		as:    newTestAutoscalerNoPC(1, 100, &metricClient{StableConcurrency: 50, PanicConcurrency: 30}),
		prepFunc: func(a *autoscaler) {
			a.panicTime = time.Now().Add(-5 * time.Second)
			a.maxPanicPods = 5
		},
		baseScale: 5,
		wantScale: 50, // Note that we use stable concurrency value for desired scale.
		wantEBC:   expectedEBC(1, 100, 30, 5),
	}, {
		label: "AutoscalerPanicStableLessThanPanic",
		as:    newTestAutoscalerNoPC(1, 100, &metricClient{StableConcurrency: 20, PanicConcurrency: 30}),
		prepFunc: func(a *autoscaler) {
			a.panicTime = time.Now().Add(-5 * time.Second)
			a.maxPanicPods = 5
		},
		baseScale: 5,
		wantScale: 30, // And here we use panic, since it's larger.
		wantEBC:   expectedEBC(1, 100, 30, 5),
	}, {
		label:     "AutoscalerStableModeNoChangeAlreadyScaled",
		as:        newTestAutoscalerNoPC(10, 100, &metricClient{StableConcurrency: 50.0, PanicConcurrency: 50}),
		baseScale: 5,
		wantScale: 5,
		wantEBC:   expectedEBC(10, 100, 50, 5),
	}, {
		label: "AutoscalerStableModeIncreaseWithSmallScaleUpRate",
		as: newTestAutoscalerNoPC(1 /* target */, 1982 /* TBC */, &metricClient{
			StableConcurrency: 3,
			PanicConcurrency:  3.1,
		}),
		baseScale: 2,
		prepFunc: func(a *autoscaler) {
			a.deciderSpec.MaxScaleUpRate = 1.1
		},
		wantScale: 3,
		wantEBC:   expectedEBC(1, 1982, 3.1, 2),
	}, {
		label:     "AutoscalerStableModeDecreaseWithSmallScaleDownRate",
		as:        newTestAutoscalerNoPC(10 /* target */, 1982 /* TBC */, &metricClient{StableConcurrency: 1, PanicConcurrency: 1}),
		baseScale: 100,
		prepFunc: func(a *autoscaler) {
			a.deciderSpec.MaxScaleDownRate = 1.1
		},
		wantScale: 90,
		wantEBC:   expectedEBC(10, 1982, 1, 100),
	}, {
		label:     "AutoscalerStableModeDecreaseNonReachable",
		as:        newTestAutoscalerNoPC(10 /* target */, 1982 /* TBC */, &metricClient{StableConcurrency: 1, PanicConcurrency: 1}),
		baseScale: 100,
		prepFunc: func(a *autoscaler) {
			a.deciderSpec.MaxScaleDownRate = 1.1
			a.deciderSpec.Reachable = false
		},
		wantScale: 1,
		wantEBC:   expectedEBC(10, 1982, 1, 100),
	}, {
		label:     "AutoscalerPanicModeDoublePodCount",
		as:        newTestAutoscalerNoPC(10, 84, &metricClient{StableConcurrency: 50, PanicConcurrency: 100}),
		baseScale: 1,
		// PanicConcurrency takes precedence.
		wantScale: 10,
		wantEBC:   expectedEBC(10, 84, 100, 1),
	}}
	for _, test := range tests {
		t.Run(test.label, func(tt *testing.T) {
			// Reset the endpoints state to the default before every test.
			test.as.podCounter.(*fakePodCounter).readyCount = test.baseScale
			if test.prepFunc != nil {
				test.prepFunc(test.as)
			}
			wantNA := expectedNA(test.as, float64(test.baseScale))
			expectScale(tt, test.as, time.Now(), ScaleResult{test.wantScale, test.wantEBC, wantNA, !test.wantInvalid})
		})
	}
}

func TestAutoscalerPanicThenUnPanicScaleDown(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 100, PanicConcurrency: 100}
	a, pc := newTestAutoscaler(10, 93, metrics)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 93, 100, 1), na, true})
	pc.readyCount = 10

	na = expectedNA(a, 10)
	panicTime := time.Now()
	metrics.PanicConcurrency = 1000
	expectScale(t, a, panicTime, ScaleResult{100, expectedEBC(10, 93, 1000, 10), na, true})

	// Traffic dropped off, scale stays as we're still in panic.
	metrics.SetStableAndPanicConcurrency(1, 1)
	expectScale(t, a, panicTime.Add(30*time.Second), ScaleResult{100, expectedEBC(10, 93, 1, 10), na, true})

	// Scale down after the StableWindow
	expectScale(t, a, panicTime.Add(61*time.Second), ScaleResult{1, expectedEBC(10, 93, 1, 10), na, true})
}

func TestAutoscalerRateLimitScaleUp(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1000, PanicConcurrency: 1001}
	a, pc := newTestAutoscaler(10, 61, metrics)
	na := expectedNA(a, 1)

	// Need 100 pods but only scale x10
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 61, 1001, 1), na, true})

	pc.readyCount = 10
	na = expectedNA(a, 10)
	// Scale x10 again
	expectScale(t, a, time.Now(), ScaleResult{100, expectedEBC(10, 61, 1001, 10), na, true})
}

func TestAutoscalerRateLimitScaleDown(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1, PanicConcurrency: 1}
	a, pc := newTestAutoscaler(10, 61, metrics)

	// Need 1 pods but can only scale down ten times, to 10.
	pc.readyCount = 100
	na := expectedNA(a, 100)
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 61, 1, 100), na, true})

	na = expectedNA(a, 10)
	pc.readyCount = 10
	// Scale ÷10 again.
	expectScale(t, a, time.Now(), ScaleResult{1, expectedEBC(10, 61, 1, 10), na, true})
}

func TestCantCountPods(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1000, PanicConcurrency: 888}
	a, pc := newTestAutoscaler(10, 81, metrics)
	pc.err = errors.New("peaches-in-regalia")
	if got, want := a.Scale(logtesting.TestLogger(t), time.Now()), invalidSR; !cmp.Equal(got, want) {
		t.Errorf("Scale = %v, want: %v", got, want)
	}
}

func TestAutoscalerUseOnePodAsMinimumIfEndpointsNotFound(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1000, PanicConcurrency: 888}
	a, pc := newTestAutoscaler(10, 81, metrics)

	pc.readyCount = 0
	// 2*10 as the rate limited if we can get the actual pods number.
	// 1*10 as the rate limited since no read pods are there from K8S API.
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 81, 888, 0), MinActivators, true})
}

func TestAutoscalerUpdateTarget(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 100, PanicConcurrency: 101}
	a, pc := newTestAutoscaler(10, 77, metrics)
	na := expectedNA(a, 1)
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 77, 101, 1), na, true})

	pc.readyCount = 10
	a.Update(&DeciderSpec{
		TargetValue:         1,
		TotalValue:          1 / targetUtilization,
		ActivatorCapacity:   21,
		TargetBurstCapacity: 71,
		PanicThreshold:      2,
		MaxScaleDownRate:    10,
		MaxScaleUpRate:      10,
		StableWindow:        stableWindow,
	})
	na = expectedNA(a, 10)
	expectScale(t, a, time.Now(), ScaleResult{100, expectedEBC(1, 71, 101, 10), na, true})
}

// For table tests and tests that don't care about changing scale.
func newTestAutoscalerNoPC(targetValue, targetBurstCapacity float64,
	metrics metrics.MetricClient) *autoscaler {
	a, _ := newTestAutoscaler(targetValue, targetBurstCapacity, metrics)
	return a
}

func newTestAutoscaler(targetValue, targetBurstCapacity float64,
	metrics metrics.MetricClient) (*autoscaler, *fakePodCounter) {
	return newTestAutoscalerWithScalingMetric(targetValue, targetBurstCapacity,
		metrics, "concurrency", false /*panic*/)
}

func newTestAutoscalerWithScalingMetric(targetValue, targetBurstCapacity float64, metrics metrics.MetricClient, metric string, startInPanic bool) (*autoscaler, *fakePodCounter) {
	deciderSpec := &DeciderSpec{
		ScalingMetric:       metric,
		TargetValue:         targetValue,
		TotalValue:          targetValue / targetUtilization, // For UTs presume 75% utilization
		TargetBurstCapacity: targetBurstCapacity,
		PanicThreshold:      2,
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    10,
		ActivatorCapacity:   activatorCapacity,
		StableWindow:        stableWindow,
		Reachable:           true,
	}

	pc := &fakePodCounter{readyCount: 1}
	// This ensures that we have endpoints object to start the autoscaler.
	if startInPanic {
		pc.readyCount = 2
	}
	ctx := smetrics.RevisionContext(testNamespace, "testSvc", "testConfig", testRevision)
	return newAutoscaler(ctx, testNamespace, testRevision, metrics, pc, deciderSpec, nil), pc
}

// approxEquateInt32 equates int32s with given path with ±-1 tolerance.
// This is needed due to rounding errors across various platforms.
func approxEquateInt32(field string) cmp.Option {
	eqOpt := cmp.Comparer(func(a, b int32) bool {
		d := a - b
		return d <= 1 && d >= -1
	})
	return cmp.FilterPath(func(p cmp.Path) bool {
		return p.String() == field
	}, eqOpt)
}

func expectScale(t *testing.T, a UniScaler, now time.Time, want ScaleResult) {
	t.Helper()
	got := a.Scale(logtesting.TestLogger(t), now)
	if !cmp.Equal(got, want, approxEquateInt32("ExcessBurstCapacity")) {
		t.Error("ScaleResult mismatch(-want,+got):\n", cmp.Diff(want, got))
	}
}

func TestStartInPanicMode(t *testing.T) {
	metrics := &staticMetricClient
	deciderSpec := &DeciderSpec{
		TargetValue:         100,
		TotalValue:          120,
		TargetBurstCapacity: 11,
		PanicThreshold:      220,
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    10,
		StableWindow:        stableWindow,
	}

	pc := &fakePodCounter{}
	for i := 0; i < 2; i++ {
		pc.readyCount = i
		a := newAutoscaler(context.Background(), testNamespace, testRevision, metrics, pc, deciderSpec, nil)
		if !a.panicTime.IsZero() {
			t.Errorf("Create at scale %d had panic mode on", i)
		}
		if got, want := int(a.maxPanicPods), i; got != want {
			t.Errorf("MaxPanicPods = %d, want: %d", got, want)
		}
	}

	// Now start with 2 and make sure we're in panic mode.
	pc.readyCount = 2
	a := newAutoscaler(context.Background(), testNamespace, testRevision, metrics, pc, deciderSpec, nil)
	if a.panicTime.IsZero() {
		t.Error("Create at scale 2 had panic mode off")
	}
	if got, want := int(a.maxPanicPods), 2; got != want {
		t.Errorf("MaxPanicPods = %d, want: %d", got, want)
	}
}

func TestNewFail(t *testing.T) {
	metrics := &staticMetricClient
	deciderSpec := &DeciderSpec{
		TargetValue:         100,
		TotalValue:          120,
		TargetBurstCapacity: 11,
		PanicThreshold:      220,
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    10,
		StableWindow:        stableWindow,
	}

	pc := fakePodCounter{err: errors.New("starlight")}
	a := newAutoscaler(context.Background(), testNamespace, testRevision, metrics, pc, deciderSpec, nil)
	if got, want := int(a.maxPanicPods), 0; got != want {
		t.Errorf("maxPanicPods = %d, want: 0", got)
	}
}

func reset() {
	metricstest.Unregister(desiredPodCountM.Name(), excessBurstCapacityM.Name(),
		stableRequestConcurrencyM.Name(),
		panicRequestConcurrencyM.Name(),
		targetRequestConcurrencyM.Name(),
		stableRPSM.Name(), panicRPSM.Name(),
		targetRPSM.Name(), panicM.Name())
	register()
}

// staticMetricClient returns stable/panic concurrency and RPS with static value, i.e. 10.
var staticMetricClient = metricClient{
	StableConcurrency: 10.0,
	PanicConcurrency:  10.0,
	StableRPS:         10.0,
	PanicRPS:          10.0,
}

// metricClient is a fake implementation of autoscaler.metricClient for testing.
type metricClient struct {
	StableConcurrency float64
	PanicConcurrency  float64
	StableRPS         float64
	PanicRPS          float64
	ErrF              func(key types.NamespacedName, now time.Time) error
}

// SetStableAndPanicConcurrency sets the stable and panic concurrencies.
func (mc *metricClient) SetStableAndPanicConcurrency(s, p float64) {
	mc.StableConcurrency, mc.PanicConcurrency = s, p
}

// StableAndPanicConcurrency returns stable/panic concurrency stored in the object
// and the result of Errf as the error.
func (mc *metricClient) StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if mc.ErrF != nil {
		err = mc.ErrF(key, now)
	}
	return mc.StableConcurrency, mc.PanicConcurrency, err
}

// StableAndPanicRPS returns stable/panic RPS stored in the object
// and the result of Errf as the error.
func (mc *metricClient) StableAndPanicRPS(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if mc.ErrF != nil {
		err = mc.ErrF(key, now)
	}
	return mc.StableRPS, mc.PanicRPS, err
}

func BenchmarkAutoscaler(b *testing.B) {
	metrics := &metricClient{StableConcurrency: 50.0, PanicConcurrency: 10}
	a := newTestAutoscalerNoPC(10, 101, metrics)
	now := time.Now()

	for i := 0; i < b.N; i++ {
		a.Scale(logtesting.TestLogger(b), now)
	}
}
