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
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"k8s.io/apimachinery/pkg/types"

	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/observability/metrics/metricstest"
	"knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/resources"
)

const (
	stableWindow      = 60 * time.Second
	targetUtilization = 0.75
	activatorCapacity = 150

	testRevision  = "a-revision-to-scale"
	testNamespace = "in-this-namespace"
)

type fakePodCounter struct {
	resources.EndpointsCounter
	readyCount int
	err        error
}

func (fpc fakePodCounter) ReadyCount() (int, error) {
	return fpc.readyCount, fpc.err
}

func TestAutoscalerScaleDownDelay(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	attrs := attribute.NewSet(attribute.String("foo", "bar"))
	pc := &fakePodCounter{}
	metrics := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      10,
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   100,
		ScaleDownDelay:   5 * time.Minute,
		Reachable:        true,
	}
	as := New(attrs, mp, testNamespace, testRevision, metrics, pc, spec)

	now := time.Time{}

	t.Run("simple", func(t *testing.T) {
		// scale up.
		metrics.SetStableAndPanicConcurrency(40, 40)
		expectScale(t, as, now.Add(2*time.Second), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4,
		})
		// five minutes pass at reduced concurrency - should not scale down (less than delay).
		metrics.SetStableAndPanicConcurrency(0, 0)
		expectScale(t, as, now.Add(5*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4,
		})
		// five minutes and 2 seconds pass at reduced concurrency - now we scale down.
		expectScale(t, as, now.Add(5*time.Minute+2*time.Second), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 0,
		})
	})

	t.Run("gradual", func(t *testing.T) {
		metrics.SetStableAndPanicConcurrency(40, 40)
		expectScale(t, as, now.Add(9*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4,
		})
		metrics.SetStableAndPanicConcurrency(30, 30)
		expectScale(t, as, now.Add(10*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4, // 4 still dominates
		})
		metrics.SetStableAndPanicConcurrency(0, 0)
		expectScale(t, as, now.Add(11*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 4, // still at 4
		})
		expectScale(t, as, now.Add(14*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 3, // 4 scrolls out, drop to 3
		})
		expectScale(t, as, now.Add(15*time.Minute), ScaleResult{
			ScaleValid:      true,
			DesiredPodCount: 0, // everything scrolled out, drop to 0
		})
	})
}

func TestAutoscalerScaleDownDelayNotReachable(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	attrs := attribute.NewSet(
		attribute.String("foo", "bar"),
	)
	pc := &fakePodCounter{}
	metrics := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      10,
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   100,
		ScaleDownDelay:   5 * time.Minute,
		Reachable:        true,
	}
	as := New(attrs, mp, testNamespace, testRevision, metrics, pc, spec)

	now := time.Time{}

	// scale up.
	metrics.SetStableAndPanicConcurrency(40, 40)
	expectScale(t, as, now.Add(2*time.Second), ScaleResult{
		ScaleValid:      true,
		DesiredPodCount: 4,
	})
	// one minute passes at reduced concurrency - should not scale down (less than delay).
	metrics.SetStableAndPanicConcurrency(0, 0)
	expectScale(t, as, now.Add(1*time.Minute), ScaleResult{
		ScaleValid:      true,
		DesiredPodCount: 4,
	})
	// mark as unreachable to simulate another revision coming up
	unreachableSpec := &DeciderSpec{
		TargetValue:      10,
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   100,
		ScaleDownDelay:   5 * time.Minute,
		Reachable:        false,
	}
	as.Update(unreachableSpec)
	// 2 seconds pass at reduced concurrency - now we scale down.
	expectScale(t, as, now.Add(2*time.Second), ScaleResult{
		ScaleValid:      true,
		DesiredPodCount: 0,
	})
}

func TestAutoscalerScaleDownDelayZero(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	attrs := attribute.NewSet(attribute.String("foo", "bar"))
	pc := &fakePodCounter{}
	metrics := &metricClient{}
	spec := &DeciderSpec{
		TargetValue:      10,
		MaxScaleDownRate: 10,
		MaxScaleUpRate:   10,
		PanicThreshold:   100,
		ScaleDownDelay:   0,
	}
	as := New(attrs, mp, testNamespace, testRevision, metrics, pc, spec)

	now := time.Time{}

	metrics.SetStableAndPanicConcurrency(40, 40)
	expectScale(t, as, now, ScaleResult{
		ScaleValid:      true,
		DesiredPodCount: 4,
	})
	// With zero delay we should immediately scale down (this is not the case
	// with a 1-element time window delay).
	metrics.SetStableAndPanicConcurrency(20, 20)
	expectScale(t, as, now.Add(500*time.Millisecond), ScaleResult{
		ScaleValid:      true,
		DesiredPodCount: 2,
	})
}

func TestAutoscalerNoDataNoAutoscale(t *testing.T) {
	metrics := &metricClient{
		ErrF: func(types.NamespacedName, time.Time) error {
			return errors.New("no metrics")
		},
	}

	a := newTestAutoscalerNoPC(10, 100, metrics)
	expectScale(t, a, time.Now(), ScaleResult{0, 0, false})
}

func expectedEBC(totCap, targetBC, recordedConcurrency, numPods float64) int32 {
	ebcF := totCap/targetUtilization*numPods - targetBC - recordedConcurrency
	// We need to floor for negative values.
	return int32(math.Floor(ebcF))
}

func TestAutoscalerStartMetrics(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 50.0, PanicConcurrency: 50.0}

	_, _, reader := newTestAutoscalerWithScalingMetric(
		10,
		100,
		metrics,
		"concurrency", true /*startInPanic*/)

	metricstest.AssertMetrics(
		t, reader,
		metricstest.MetricsEqual(scopeName, panicMetric(true)),
	)
}

func TestAutoscalerMetrics(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 50.0, PanicConcurrency: 50.0}
	a, _, reader := newTestAutoscaler(10, 100, metrics)
	expectedAttrs := attribute.NewSet(attribute.String("foo", "bar"))

	// Non-panic created autoscaler.
	metricstest.AssertMetrics(
		t, reader,
		metricstest.MetricsEqual(scopeName, panicMetric(false)),
	)

	ebc := expectedEBC(10, 100, 50, 1)
	expectScale(t, a, time.Now(), ScaleResult{5, ebc, true})
	spec := a.currentSpec()

	metricstest.AssertMetrics(
		t, reader,
		metricstest.MetricsEqual(
			scopeName,
			panicMetric(true),

			metricdata.Metrics{
				Name:        "kn.revision.pods.desired",
				Description: "Number of pods the autoscaler wants to allocate",
				Unit:        "{item}",
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{{
						Attributes: expectedAttrs,
						Value:      5,
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.capacity.excess",
				Description: "Excess burst capacity observed over the stable window",
				Unit:        "{concurrency}",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      float64(ebc),
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.concurrency.stable",
				Description: "Average of request count per observed pod over the stable window",
				Unit:        "{concurrency}",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      50,
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.concurrency.panic",
				Description: "Average of request count per observed pod over the panic window",
				Unit:        "{concurrency}",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      50,
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.concurrency.target",
				Description: "The desired concurrent requests for each pod",
				Unit:        "{concurrency}",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      float64(spec.TargetValue),
					}},
				},
			},
		),
	)
}

func TestAutoscalerMetricsWithRPS(t *testing.T) {
	expectedAttrs := attribute.NewSet(attribute.String("foo", "bar"))
	metrics := &metricClient{PanicRPS: 99.0, StableRPS: 100}
	a, _, reader := newTestAutoscalerWithScalingMetric(10, 100, metrics, "rps", false /*startInPanic*/)
	ebc := expectedEBC(10, 100, 99, 1)
	spec := a.currentSpec()

	expectScale(t, a, time.Now().Add(61*time.Second), ScaleResult{10, ebc, true})

	metricstest.AssertMetrics(
		t, reader,
		metricstest.MetricsEqual(
			scopeName,
			panicMetric(true),

			metricdata.Metrics{
				Name:        "kn.revision.pods.desired",
				Description: "Number of pods the autoscaler wants to allocate",
				Unit:        "{item}",
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{{
						Attributes: expectedAttrs,
						Value:      10,
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.capacity.excess",
				Description: "Excess burst capacity observed over the stable window",
				Unit:        "{concurrency}",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      float64(ebc),
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.rps.stable",
				Description: "Average of requests-per-second per observed pod over the stable window",
				Unit:        "{request}/s",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      100,
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.rps.panic",
				Description: "Average of requests-per-second per observed pod over the panic window",
				Unit:        "{request}/s",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      99,
					}},
				},
			},

			metricdata.Metrics{
				Name:        "kn.revision.rps.target",
				Description: "The desired concurrent requests for each pod",
				Unit:        "{request}/s",
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{{
						Attributes: expectedAttrs,
						Value:      float64(spec.TargetValue),
					}},
				},
			},
		),
	)
}

func TestAutoscalerStableModeIncreaseWithConcurrencyDefault(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 50.0, PanicConcurrency: 10}
	a := newTestAutoscalerNoPC(10, 101, metrics)
	expectScale(t, a, time.Now(), ScaleResult{5, expectedEBC(10, 101, 10, 1), true})

	metrics.StableConcurrency = 100
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 101, 10, 1), true})
}

func TestAutoscalerStableModeIncreaseWithRPS(t *testing.T) {
	metrics := &metricClient{StableRPS: 50.0, PanicRPS: 50}
	a, _, _ := newTestAutoscalerWithScalingMetric(10, 101, metrics, "rps", false /*startInPanic*/)
	expectScale(t, a, time.Now(), ScaleResult{5, expectedEBC(10, 101, 50, 1), true})

	metrics.StableRPS = 100
	metrics.PanicRPS = 99
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 101, 99, 1), true})
}

func TestAutoscalerUnpanicAfterSlowIncrease(t *testing.T) {
	// Do initial jump from 10 to 25 pods.
	metrics := &metricClient{StableConcurrency: 11, PanicConcurrency: 25}
	a, pc, _ := newTestAutoscaler(1, 98, metrics)
	pc.readyCount = 10

	start := time.Now()
	tm := start
	expectScale(t, a, tm, ScaleResult{25, expectedEBC(1, 98, 25, 10), true})
	if a.panicTime != tm {
		t.Errorf("PanicTime = %v, want: %v", a.panicTime, tm)
	}
	// Now the half of the stable window has passed, and we've been adding 1 pod per cycle.
	// For window of 60s, that's +15 pods.
	pc.readyCount = 25 + 15

	metrics.SetStableAndPanicConcurrency(30, 41)
	tm = tm.Add(stableWindow / 2)

	expectScale(t, a, tm, ScaleResult{41, expectedEBC(1, 98, 41, 40), true})
	if a.panicTime != start {
		t.Error("Panic Time should not have moved")
	}

	// Now at the end. Panic must end. And +30 pods.
	pc.readyCount = 25 + 30
	metrics.SetStableAndPanicConcurrency(50, 56)
	tm = tm.Add(stableWindow/2 + tickInterval)

	expectScale(t, a, tm, ScaleResult{50 /* no longer in panic*/, expectedEBC(1, 98, 56, 55), true})
	if !a.panicTime.IsZero() {
		t.Errorf("PanicTime = %v, want: 0", a.panicTime)
	}
}

func TestAutoscalerExtendPanicWindow(t *testing.T) {
	// Do initial jump from 10 to 25 pods.
	metrics := &metricClient{StableConcurrency: 11, PanicConcurrency: 25}
	a, pc, _ := newTestAutoscaler(1, 98, metrics)
	pc.readyCount = 10

	start := time.Now()
	tm := start
	expectScale(t, a, tm, ScaleResult{25, expectedEBC(1, 98, 25, 10), true})
	if a.panicTime != tm {
		t.Errorf("PanicTime = %v, want: %v", a.panicTime, tm)
	}
	// Now the half of the stable window has passed, and we're still surging.
	pc.readyCount = 25 + 15

	metrics.SetStableAndPanicConcurrency(30, 80)
	tm = tm.Add(stableWindow / 2)

	expectScale(t, a, tm, ScaleResult{80, expectedEBC(1, 98, 80, 40), true})
	if a.panicTime != tm {
		t.Errorf("PanicTime = %v, want: %v", a.panicTime, tm)
	}
}

func TestAutoscalerStableModeDecrease(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 100.0, PanicConcurrency: 100}
	a, pc, _ := newTestAutoscaler(10, 98, metrics)
	pc.readyCount = 8
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 98, 100, 8), true})

	metrics.SetStableAndPanicConcurrency(50, 50)
	expectScale(t, a, time.Now(), ScaleResult{5, expectedEBC(10, 98, 50, 8), true})
}

func TestAutoscalerStableModeNoTrafficScaleToZero(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1, PanicConcurrency: 0}
	a := newTestAutoscalerNoPC(10, 75, metrics)
	expectScale(t, a, time.Now(), ScaleResult{1, expectedEBC(10, 75, 0, 1), true})

	metrics.StableConcurrency = 0.0
	expectScale(t, a, time.Now(), ScaleResult{0, expectedEBC(10, 75, 0, 1), true})
}

func TestAutoscalerActivationScale(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 0, PanicConcurrency: 0}
	a := newTestAutoscalerNoPC(10, 75, metrics)
	a.deciderSpec.ActivationScale = int32(2)
	expectScale(t, a, time.Now(), ScaleResult{0, expectedEBC(10, 75, 0, 1), true})

	metrics.StableConcurrency = 1.0
	expectScale(t, a, time.Now(), ScaleResult{2, expectedEBC(10, 75, 0, 1), true})
}

// QPS is increasing exponentially. Each scaling event bring concurrency
// back to the target level (1.0) but then traffic continues to increase.
// At 1296 QPS traffic stabilizes.
func TestAutoscalerPanicModeExponentialTrackAndStabilize(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 6, PanicConcurrency: 6}
	a, pc, _ := newTestAutoscaler(1, 101, metrics)
	expectScale(t, a, time.Now(), ScaleResult{6, expectedEBC(1, 101, 6, 1), true})

	tm := time.Now()
	pc.readyCount = 6
	metrics.SetStableAndPanicConcurrency(36, 36)
	expectScale(t, a, tm, ScaleResult{36, expectedEBC(1, 101, 36, 6), true})
	if got, want := a.panicTime, tm; got != tm {
		t.Errorf("PanicTime = %v, want: %v", got, want)
	}

	pc.readyCount = 36
	metrics.SetStableAndPanicConcurrency(216, 216)
	tm = tm.Add(time.Second)
	expectScale(t, a, tm, ScaleResult{216, expectedEBC(1, 101, 216, 36), true})
	if got, want := a.panicTime, tm; got != tm {
		t.Errorf("PanicTime = %v, want: %v", got, want)
	}

	pc.readyCount = 216
	metrics.SetStableAndPanicConcurrency(1296, 1296)
	expectScale(t, a, tm, ScaleResult{1296, expectedEBC(1, 101, 1296, 216), true})
	if got, want := a.panicTime, tm; got != tm {
		t.Errorf("PanicTime = %v, want: %v", got, want)
	}

	pc.readyCount = 1296
	tm = tm.Add(time.Second)
	expectScale(t, a, tm, ScaleResult{1296, expectedEBC(1, 101, 1296, 1296), true})
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
			expectScale(tt, test.as, time.Now(), ScaleResult{test.wantScale, test.wantEBC, !test.wantInvalid})
		})
	}
}

func TestAutoscalerPanicThenUnPanicScaleDown(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 100, PanicConcurrency: 100}
	a, pc, _ := newTestAutoscaler(10, 93, metrics)
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 93, 100, 1), true})
	pc.readyCount = 10

	panicTime := time.Now()
	metrics.PanicConcurrency = 1000
	expectScale(t, a, panicTime, ScaleResult{100, expectedEBC(10, 93, 1000, 10), true})

	// Traffic dropped off, scale stays as we're still in panic.
	metrics.SetStableAndPanicConcurrency(1, 1)
	expectScale(t, a, panicTime.Add(30*time.Second), ScaleResult{100, expectedEBC(10, 93, 1, 10), true})

	// Scale down after the StableWindow
	expectScale(t, a, panicTime.Add(61*time.Second), ScaleResult{1, expectedEBC(10, 93, 1, 10), true})
}

func TestAutoscalerRateLimitScaleUp(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1000, PanicConcurrency: 1001}
	a, pc, _ := newTestAutoscaler(10, 61, metrics)

	// Need 100 pods but only scale x10
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 61, 1001, 1), true})

	pc.readyCount = 10
	// Scale x10 again
	expectScale(t, a, time.Now(), ScaleResult{100, expectedEBC(10, 61, 1001, 10), true})
}

func TestAutoscalerRateLimitScaleDown(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1, PanicConcurrency: 1}
	a, pc, _ := newTestAutoscaler(10, 61, metrics)

	// Need 1 pods but can only scale down ten times, to 10.
	pc.readyCount = 100
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 61, 1, 100), true})

	pc.readyCount = 10
	// Scale ÷10 again.
	expectScale(t, a, time.Now(), ScaleResult{1, expectedEBC(10, 61, 1, 10), true})
}

func TestCantCountPods(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1000, PanicConcurrency: 888}
	a, pc, _ := newTestAutoscaler(10, 81, metrics)
	pc.err = errors.New("peaches-in-regalia")
	if got, want := a.Scale(logtesting.TestLogger(t), time.Now()), invalidSR; !cmp.Equal(got, want) {
		t.Errorf("Scale = %v, want: %v", got, want)
	}
}

func TestAutoscalerUseOnePodAsMinimumIfEndpointsNotFound(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 1000, PanicConcurrency: 888}
	a, pc, _ := newTestAutoscaler(10, 81, metrics)

	pc.readyCount = 0
	// 2*10 as the rate limited if we can get the actual pods number.
	// 1*10 as the rate limited since no read pods are there from K8S API.
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 81, 888, 0), true})
}

func TestAutoscalerUpdateTarget(t *testing.T) {
	metrics := &metricClient{StableConcurrency: 100, PanicConcurrency: 101}
	a, pc, _ := newTestAutoscaler(10, 77, metrics)
	expectScale(t, a, time.Now(), ScaleResult{10, expectedEBC(10, 77, 101, 1), true})

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
	expectScale(t, a, time.Now(), ScaleResult{100, expectedEBC(1, 71, 101, 10), true})
}

// For table tests and tests that don't care about changing scale.
func newTestAutoscalerNoPC(
	targetValue float64,
	targetBurstCapacity float64,
	metrics metrics.MetricClient,
) *autoscaler {
	a, _, _ := newTestAutoscaler(targetValue, targetBurstCapacity, metrics)
	return a
}

func newTestAutoscaler(
	targetValue float64,
	targetBurstCapacity float64,
	metrics metrics.MetricClient,
) (*autoscaler, *fakePodCounter, *metric.ManualReader) {
	return newTestAutoscalerWithScalingMetric(
		targetValue,
		targetBurstCapacity,
		metrics, "concurrency",
		false /*panic*/)
}

func newTestAutoscalerWithScalingMetric(
	targetValue,
	targetBurstCapacity float64,
	metrics metrics.MetricClient,
	scalingMetric string,
	startInPanic bool,
) (*autoscaler, *fakePodCounter, *metric.ManualReader) {
	deciderSpec := &DeciderSpec{
		ScalingMetric:       scalingMetric,
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

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	attrs := attribute.NewSet(attribute.String("foo", "bar"))

	return newAutoscaler(attrs, mp, testNamespace, testRevision, metrics, pc, deciderSpec, nil), pc, reader
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
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	attrs := attribute.NewSet(attribute.String("foo", "bar"))
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
	for i := range 2 {
		pc.readyCount = i
		a := newAutoscaler(attrs, mp, testNamespace, testRevision, metrics, pc, deciderSpec, nil)
		if !a.panicTime.IsZero() {
			t.Errorf("Create at scale %d had panic mode on", i)
		}
		if got, want := int(a.maxPanicPods), i; got != want {
			t.Errorf("MaxPanicPods = %d, want: %d", got, want)
		}
	}

	// Now start with 2 and make sure we're in panic mode.
	pc.readyCount = 2
	a := newAutoscaler(attrs, mp, testNamespace, testRevision, metrics, pc, deciderSpec, nil)
	if a.panicTime.IsZero() {
		t.Error("Create at scale 2 had panic mode off")
	}
	if got, want := int(a.maxPanicPods), 2; got != want {
		t.Errorf("MaxPanicPods = %d, want: %d", got, want)
	}
}

func TestNewFail(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	attrs := attribute.NewSet(attribute.String("foo", "bar"))
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
	a := newAutoscaler(attrs, mp, testNamespace, testRevision, metrics, pc, deciderSpec, nil)
	if got, want := int(a.maxPanicPods), 0; got != want {
		t.Errorf("maxPanicPods = %d, want: 0", got)
	}
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

	for b.Loop() {
		a.Scale(logtesting.TestLogger(b), now)
	}
}

func panicMetric(panic bool) metricdata.Metrics {
	val := 0
	if panic {
		val = 1
	}
	return metricdata.Metrics{
		Name:        "kn.revision.panic.mode",
		Description: "If greater tha 0 the autoscaler is in panic mode",
		Data: metricdata.Gauge[int64]{
			DataPoints: []metricdata.DataPoint[int64]{{
				Value:      int64(val),
				Attributes: attribute.NewSet(attribute.String("foo", "bar")),
			}},
		},
	}
}
