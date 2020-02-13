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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/kmp"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/autoscaler/fake"
	autoscalerfake "knative.dev/serving/pkg/autoscaler/fake"
	"knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	stableWindow      = 60 * time.Second
	targetUtilization = 0.75
)

func TestNewErrorWhenGivenNilReadyPodCounter(t *testing.T) {
	if _, err := New(fake.TestNamespace, fake.TestRevision, &autoscalerfake.MetricClient{}, nil, &DeciderSpec{TargetValue: 10, ServiceName: fake.TestService}, context.Background()); err == nil {
		t.Error("Expected error when ReadyPodCounter interface is nil, but got none.")
	}
}

func TestNewErrorWhenGivenNilStatsReporter(t *testing.T) {
	l := fake.KubeInformer.Core().V1().Endpoints().Lister()
	if _, err := New(fake.TestNamespace, fake.TestRevision, &autoscalerfake.MetricClient{}, l,
		&DeciderSpec{TargetValue: 10, ServiceName: fake.TestService}, nil); err == nil {
		t.Error("Expected error when EndpointsInformer interface is nil, but got none.")
	}
}

func TestAutoscalerNoDataNoAutoscale(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{
		ErrF: func(key types.NamespacedName, now time.Time) error {
			return errors.New("no metrics")
		},
	}

	a := newTestAutoscaler(t, 10, 100, metrics)
	a.expectScale(t, time.Now(), 0, 0, false)
}

func expectedEBC(totCap, targetBC, recordedConcurrency, numPods float64) int32 {
	return int32(math.Floor(totCap/targetUtilization*numPods - targetBC - recordedConcurrency))
}

func TestAutoscalerChangeOfPodCountService(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 50.0}
	a := newTestAutoscaler(t, 10, 100, metrics)
	a.expectScale(t, time.Now(), 5, expectedEBC(10, 100, 50, 1), true)

	const newTS = fake.TestService + "2"
	newDS := *a.deciderSpec
	newDS.ServiceName = newTS
	a.Update(&newDS)

	// Make two pods in the new service.
	fake.Endpoints(2, newTS)
	// This should change the EBC computation, but target scale doesn't change.
	a.expectScale(t, time.Now(), 5, expectedEBC(10, 100, 50, 2), true)
}

func TestAutoscalerStableModeIncreaseWithConcurrencyDefault(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 50.0}
	a := newTestAutoscaler(t, 10, 101, metrics)
	a.expectScale(t, time.Now(), 5, expectedEBC(10, 101, 50, 1), true)

	metrics.StableConcurrency = 100
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 101, 100, 1), true)
}

func TestAutoscalerStableModeIncreaseWithRPS(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableRPS: 50.0}
	a := newTestAutoscalerWithScalingMetric(t, 10, 101, metrics, "rps")
	a.expectScale(t, time.Now(), 5, expectedEBC(10, 101, 50, 1), true)

	metrics.StableRPS = 100
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 101, 100, 1), true)
}

func TestAutoscalerStableModeDecrease(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 100.0}
	a := newTestAutoscaler(t, 10, 98, metrics)
	fake.Endpoints(8, fake.TestService)
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 98, 100, 8), true)

	metrics.StableConcurrency = 50
	a.expectScale(t, time.Now(), 5, expectedEBC(10, 98, 50, 8), true)
}

func TestAutoscalerStableModeNoTrafficScaleToZero(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 1}
	a := newTestAutoscaler(t, 10, 75, metrics)
	a.expectScale(t, time.Now(), 1, expectedEBC(10, 75, 1, 1), true)

	metrics.StableConcurrency = 0.0
	a.expectScale(t, time.Now(), 0, expectedEBC(10, 75, 0, 1), true)
}

// QPS is increasing exponentially. Each scaling event bring concurrency
// back to the target level (1.0) but then traffic continues to increase.
// At 1296 QPS traffic stablizes.
func TestAutoscalerPanicModeExponentialTrackAndStablize(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 6, PanicConcurrency: 6}
	a := newTestAutoscaler(t, 1, 101, metrics)
	a.expectScale(t, time.Now(), 6, expectedEBC(1, 101, 6, 1), true)

	fake.Endpoints(6, fake.TestService)
	metrics.PanicConcurrency, metrics.StableConcurrency = 36, 36
	a.expectScale(t, time.Now(), 36, expectedEBC(1, 101, 36, 6), true)

	fake.Endpoints(36, fake.TestService)
	metrics.PanicConcurrency, metrics.StableConcurrency = 216, 216
	a.expectScale(t, time.Now(), 216, expectedEBC(1, 101, 216, 36), true)

	fake.Endpoints(216, fake.TestService)
	metrics.PanicConcurrency, metrics.StableConcurrency = 1296, 1296
	a.expectScale(t, time.Now(), 1296, expectedEBC(1, 101, 1296, 216), true)
	fake.Endpoints(1296, fake.TestService)
	a.expectScale(t, time.Now(), 1296, expectedEBC(1, 101, 1296, 1296), true)
}

func TestAutoscalerScale(t *testing.T) {
	tests := []struct {
		label       string
		as          *Autoscaler
		prepFunc    func(as *Autoscaler)
		wantScale   int32
		wantEBC     int32
		wantInvalid bool
	}{{
		label:     "AutoscalerNoDataAtZeroNoAutoscale",
		as:        newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{}),
		wantScale: 0,
		wantEBC:   expectedEBC(10, 100, 0, 1),
	}, {
		label:     "AutoscalerNoDataAtZeroNoAutoscaleWithExplicitEPs",
		as:        newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{}),
		prepFunc:  func(*Autoscaler) { fake.Endpoints(1, fake.TestService) },
		wantScale: 0,
		wantEBC:   expectedEBC(10, 100, 0, 1),
	}, {
		label:     "AutoscalerStableModeUnlimitedTBC",
		as:        newTestAutoscaler(t, 181, -1, &autoscalerfake.MetricClient{StableConcurrency: 21.0}),
		wantScale: 1,
		wantEBC:   -1,
	}, {
		label:     "Autoscaler0TBC",
		as:        newTestAutoscaler(t, 10, 0, &autoscalerfake.MetricClient{StableConcurrency: 50.0}),
		wantScale: 5,
		wantEBC:   0,
	}, {
		label:     "AutoscalerStableModeNoChange",
		as:        newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{StableConcurrency: 50.0}),
		wantScale: 5,
		wantEBC:   expectedEBC(10, 100, 50, 1),
	}, {
		label:     "AutoscalerStableModeNoChangeAlreadyScaled",
		as:        newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{StableConcurrency: 50.0}),
		prepFunc:  func(*Autoscaler) { fake.Endpoints(5, fake.TestService) },
		wantScale: 5,
		wantEBC:   expectedEBC(10, 100, 50, 5),
	}, {
		label:     "AutoscalerStableModeNoChangeAlreadyScaled",
		as:        newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{StableConcurrency: 50.0}),
		prepFunc:  func(*Autoscaler) { fake.Endpoints(5, fake.TestService) },
		wantScale: 5,
		wantEBC:   expectedEBC(10, 100, 50, 5),
	}, {
		label: "AutoscalerStableModeIncreaseWithSmallScaleUpRate",
		as:    newTestAutoscaler(t, 1 /* target */, 1982 /* TBC */, &autoscalerfake.MetricClient{StableConcurrency: 3}),
		prepFunc: func(a *Autoscaler) {
			a.deciderSpec.MaxScaleUpRate = 1.1
			fake.Endpoints(2, fake.TestService)
		},
		wantScale: 3,
		wantEBC:   expectedEBC(1, 1982, 3, 2),
	}, {
		label: "AutoscalerStableModeIncreaseWithSmallScaleDownRate",
		as:    newTestAutoscaler(t, 10 /* target */, 1982 /* TBC */, &autoscalerfake.MetricClient{StableConcurrency: 1}),
		prepFunc: func(a *Autoscaler) {
			a.deciderSpec.MaxScaleDownRate = 1.1
			fake.Endpoints(100, fake.TestService)
		},
		wantScale: 90,
		wantEBC:   expectedEBC(10, 1982, 1, 100),
	}, {
		label: "AutoscalerPanicModeDoublePodCount",
		as:    newTestAutoscaler(t, 10, 84, &autoscalerfake.MetricClient{StableConcurrency: 50, PanicConcurrency: 100}),
		// PanicConcurrency takes precedence.
		wantScale: 10,
		wantEBC:   expectedEBC(10, 84, 50, 1),
	}}
	for _, test := range tests {
		t.Run(test.label, func(tt *testing.T) {
			// Reset the endpoints state to the default before every test.
			fake.Endpoints(1, fake.TestService)
			if test.prepFunc != nil {
				test.prepFunc(test.as)
			}
			test.as.expectScale(tt, time.Now(), test.wantScale, test.wantEBC, !test.wantInvalid)
		})
	}
}

func TestAutoscalerPanicThenUnPanicScaleDown(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 100, PanicConcurrency: 100}
	a := newTestAutoscaler(t, 10, 93, metrics)
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 93, 100, 1), true)
	fake.Endpoints(10, fake.TestService)

	panicTime := time.Now()
	metrics.PanicConcurrency = 1000
	a.expectScale(t, panicTime, 100, expectedEBC(10, 93, 100, 10), true)

	// Traffic dropped off, scale stays as we're still in panic.
	metrics.PanicConcurrency = 1
	metrics.StableConcurrency = 1
	a.expectScale(t, panicTime.Add(30*time.Second), 100, expectedEBC(10, 93, 1, 10), true)

	// Scale down after the StableWindow
	a.expectScale(t, panicTime.Add(61*time.Second), 1, expectedEBC(10, 93, 1, 10), true)
}

func TestAutoscalerRateLimitScaleUp(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 1000}
	a := newTestAutoscaler(t, 10, 61, metrics)

	// Need 100 pods but only scale x10
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 61, 1000, 1), true)

	fake.Endpoints(10, fake.TestService)
	// Scale x10 again
	a.expectScale(t, time.Now(), 100, expectedEBC(10, 61, 1000, 10), true)
}

func TestAutoscalerRateLimitScaleDown(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 1}
	a := newTestAutoscaler(t, 10, 61, metrics)

	// Need 1 pods but can only scale down ten times, to 10.
	fake.Endpoints(100, fake.TestService)
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 61, 1, 100), true)

	fake.Endpoints(10, fake.TestService)
	// Scale ÷10 again.
	a.expectScale(t, time.Now(), 1, expectedEBC(10, 61, 1, 10), true)
}

func eraseEndpoints() {
	ep, _ := fake.KubeClient.CoreV1().Endpoints(fake.TestNamespace).Get(fake.TestService, metav1.GetOptions{})
	fake.KubeClient.CoreV1().Endpoints(fake.TestNamespace).Delete(fake.TestService, nil)
	fake.KubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Delete(ep)
}

func TestAutoscalerUseOnePodAsMinimumIfEndpointsNotFound(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 1000}
	a := newTestAutoscaler(t, 10, 81, metrics)

	fake.Endpoints(0, fake.TestService)
	// 2*10 as the rate limited if we can get the actual pods number.
	// 1*10 as the rate limited since no read pods are there from K8S API.
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 81, 1000, 0), true)

	eraseEndpoints()
	// 2*10 as the rate limited if we can get the actual pods number.
	// 1*10 as the rate limited since no Endpoints object is there from K8S API.
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 81, 1000, 0), true)
}

func TestAutoscalerUpdateTarget(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 100}
	a := newTestAutoscaler(t, 10, 77, metrics)
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 77, 100, 1), true)

	fake.Endpoints(10, fake.TestService)
	a.Update(&DeciderSpec{
		TargetValue:         1,
		TotalValue:          1 / targetUtilization,
		TargetBurstCapacity: 71,
		PanicThreshold:      2,
		MaxScaleDownRate:    10,
		MaxScaleUpRate:      10,
		StableWindow:        stableWindow,
		ServiceName:         fake.TestService,
	})
	a.expectScale(t, time.Now(), 100, expectedEBC(1, 71, 100, 10), true)
}

func newTestAutoscaler(t *testing.T, targetValue, targetBurstCapacity float64, metrics metrics.MetricClient) *Autoscaler {
	return newTestAutoscalerWithScalingMetric(t, targetValue, targetBurstCapacity, metrics, "concurrency")
}

func newTestAutoscalerWithScalingMetric(t *testing.T, targetValue, targetBurstCapacity float64, metrics metrics.MetricClient, metric string) *Autoscaler {
	t.Helper()
	deciderSpec := &DeciderSpec{
		ScalingMetric:       metric,
		TargetValue:         targetValue,
		TotalValue:          targetValue / targetUtilization, // For UTs presume 75% utilization
		TargetBurstCapacity: targetBurstCapacity,
		PanicThreshold:      2 * targetValue,
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    10,
		StableWindow:        stableWindow,
		ServiceName:         fake.TestService,
	}

	l := fake.KubeInformer.Core().V1().Endpoints().Lister()
	// This ensures that we have endpoints object to start the autoscaler.
	fake.Endpoints(0, fake.TestService)
	a, err := New(fake.TestNamespace, fake.TestRevision, metrics, l, deciderSpec, context.Background())
	if err != nil {
		t.Fatalf("Error creating test autoscaler: %v", err)
	}
	fake.Endpoints(1, fake.TestService)
	return a
}

func (a *Autoscaler) expectScale(t *testing.T, now time.Time, expectScale, expectEBC int32, expectOK bool) {
	t.Helper()
	scale, ebc, ok := a.Scale(TestContextWithLogger(t), now)
	if ok != expectOK {
		t.Errorf("Unexpected autoscale decision. Expected %v. Got %v.", expectOK, ok)
	}
	if got, want := scale, expectScale; got != want {
		t.Errorf("Scale %d, want: %d", got, want)
	}
	if got, want := ebc, expectEBC; got != want {
		t.Errorf("ExcessBurstCapacity = %d, want: %d", got, want)
	}
}

func TestStartInPanicMode(t *testing.T) {
	metrics := &autoscalerfake.StaticMetricClient
	deciderSpec := &DeciderSpec{
		TargetValue:         100,
		TotalValue:          120,
		TargetBurstCapacity: 11,
		PanicThreshold:      220,
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    10,
		StableWindow:        stableWindow,
		ServiceName:         fake.TestService,
	}

	l := fake.KubeInformer.Core().V1().Endpoints().Lister()
	for i := 0; i < 2; i++ {
		fake.Endpoints(i, fake.TestService)
		a, err := New(fake.TestNamespace, fake.TestRevision, metrics, l, deciderSpec, context.Background())
		if err != nil {
			t.Fatalf("Error creating test autoscaler: %v", err)
		}
		if !a.panicTime.IsZero() {
			t.Errorf("Create at scale %d had panic mode on", i)
		}
		if got, want := int(a.maxPanicPods), i; got != want {
			t.Errorf("MaxPanicPods = %d, want: %d", got, want)
		}
	}

	// Now start with 2 and make sure we're in panic mode.
	fake.Endpoints(2, fake.TestService)
	a, err := New(fake.TestNamespace, fake.TestRevision, metrics, l, deciderSpec, context.Background())
	if err != nil {
		t.Fatalf("Error creating test autoscaler: %v", err)
	}
	if a.panicTime.IsZero() {
		t.Error("Create at scale 2 had panic mode off")
	}
	if got, want := int(a.maxPanicPods), 2; got != want {
		t.Errorf("MaxPanicPods = %d, want: %d", got, want)
	}
}

func TestNewFail(t *testing.T) {
	eraseEndpoints()
	metrics := &autoscalerfake.StaticMetricClient
	deciderSpec := &DeciderSpec{
		TargetValue:         100,
		TotalValue:          120,
		TargetBurstCapacity: 11,
		PanicThreshold:      220,
		MaxScaleUpRate:      10,
		MaxScaleDownRate:    10,
		StableWindow:        stableWindow,
		ServiceName:         fake.TestService,
	}

	l := fake.KubeInformer.Core().V1().Endpoints().Lister()
	a, err := New(fake.TestNamespace, fake.TestRevision, metrics, l, deciderSpec, context.Background())
	if err != nil {
		t.Errorf("No endpoints should succeed, err = %v", err)
	}
	if got, want := int(a.maxPanicPods), 0; got != want {
		t.Errorf("maxPanicPods = %d, want: 0", got)
	}
}

func TestPrepareForRemoval(t *testing.T) {
	var expectedCandidates = []string{"pod1"}
	metrics := &autoscalerfake.MetricClient{RemovalCandidates: expectedCandidates}

	t.Run("when not scaling down", func(t *testing.T) {
		a := newTestAutoscaler(t, 10, 77, metrics)
		candidates, err := a.PrepareForRemoval(TestContextWithLogger(t), 10)
		if err != nil || candidates != nil {
			t.Errorf("PrepareForRemoval() had to be empty, got = %v, err = %v", candidates, err)
		}
	})

	t.Run("when scaling down", func(t *testing.T) {
		a := newTestAutoscaler(t, 10, 77, metrics)
		candidates, err := a.PrepareForRemoval(TestContextWithLogger(t), 0)
		if err != nil {
			t.Errorf("PrepareForRemoval() has error = %v", err)
		}

		if identical, err := kmp.SafeEqual(candidates, expectedCandidates); err != nil || !identical {
			t.Errorf("PrepareForRemoval() wanted = %v, got = %v, err = %v", expectedCandidates, candidates, err)
		}
	})
}
