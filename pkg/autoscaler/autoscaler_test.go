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
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
	. "knative.dev/pkg/logging/testing"
	autoscalerfake "knative.dev/serving/pkg/autoscaler/fake"
)

const (
	stableWindow      = 60 * time.Second
	targetUtilization = 0.75
)

var (
	kubeClient   = fakeK8s.NewSimpleClientset()
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
)

func TestNewErrorWhenGivenNilReadyPodCounter(t *testing.T) {
	_, err := New(testNamespace, testRevision, &autoscalerfake.MetricClient{}, nil, &DeciderSpec{TargetValue: 10, ServiceName: testService}, &mockReporter{})
	if err == nil {
		t.Error("Expected error when ReadyPodCounter interface is nil, but got none.")
	}
}

func TestNewErrorWhenGivenNilStatsReporter(t *testing.T) {
	var reporter StatsReporter

	l := kubeInformer.Core().V1().Endpoints().Lister()
	_, err := New(testNamespace, testRevision, &autoscalerfake.MetricClient{}, l,
		&DeciderSpec{TargetValue: 10, ServiceName: testService}, reporter)
	if err == nil {
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

	const newTS = testService + "2"
	newDS := *a.deciderSpec
	newDS.ServiceName = newTS
	a.Update(&newDS)

	// Make two pods in the new service.
	endpoints(2, newTS)
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
	endpoints(8, testService)
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

	endpoints(6, testService)
	metrics.PanicConcurrency, metrics.StableConcurrency = 36, 36
	a.expectScale(t, time.Now(), 36, expectedEBC(1, 101, 36, 6), true)

	endpoints(36, testService)
	metrics.PanicConcurrency, metrics.StableConcurrency = 216, 216
	a.expectScale(t, time.Now(), 216, expectedEBC(1, 101, 216, 36), true)

	endpoints(216, testService)
	metrics.PanicConcurrency, metrics.StableConcurrency = 1296, 1296
	a.expectScale(t, time.Now(), 1296, expectedEBC(1, 101, 1296, 216), true)
	endpoints(1296, testService)
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
		prepFunc:  func(*Autoscaler) { endpoints(1, testService) },
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
		prepFunc:  func(*Autoscaler) { endpoints(5, testService) },
		wantScale: 5,
		wantEBC:   expectedEBC(10, 100, 50, 5),
	}, {
		label:     "AutoscalerStableModeNoChangeAlreadyScaled",
		as:        newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{StableConcurrency: 50.0}),
		prepFunc:  func(*Autoscaler) { endpoints(5, testService) },
		wantScale: 5,
		wantEBC:   expectedEBC(10, 100, 50, 5),
	}, {
		label: "AutoscalerStableModeIncreaseWithSmallScaleUpRate",
		as:    newTestAutoscaler(t, 1 /* target */, 1982 /* TBC */, &autoscalerfake.MetricClient{StableConcurrency: 3}),
		prepFunc: func(a *Autoscaler) {
			a.deciderSpec.MaxScaleUpRate = 1.1
			endpoints(2, testService)
		},
		wantScale: 3,
		wantEBC:   expectedEBC(1, 1982, 3, 2),
	}, {
		label: "AutoscalerStableModeIncreaseWithSmallScaleDownRate",
		as:    newTestAutoscaler(t, 10 /* target */, 1982 /* TBC */, &autoscalerfake.MetricClient{StableConcurrency: 1}),
		prepFunc: func(a *Autoscaler) {
			a.deciderSpec.MaxScaleDownRate = 1.1
			endpoints(100, testService)
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
			endpoints(1, testService)
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
	endpoints(10, testService)

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

	endpoints(10, testService)
	// Scale x10 again
	a.expectScale(t, time.Now(), 100, expectedEBC(10, 61, 1000, 10), true)
}

func TestAutoscalerRateLimitScaleDown(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 1}
	a := newTestAutoscaler(t, 10, 61, metrics)

	// Need 1 pods but can only scale down ten times, to 10.
	endpoints(100, testService)
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 61, 1, 100), true)

	endpoints(10, testService)
	// Scale ÷10 again.
	a.expectScale(t, time.Now(), 1, expectedEBC(10, 61, 1, 10), true)
}

func eraseEndpoints() {
	ep, _ := kubeClient.CoreV1().Endpoints(testNamespace).Get(testService, metav1.GetOptions{})
	kubeClient.CoreV1().Endpoints(testNamespace).Delete(testService, nil)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Delete(ep)
}

func TestAutoscalerUseOnePodAsMinimumIfEndpointsNotFound(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 1000}
	a := newTestAutoscaler(t, 10, 81, metrics)

	endpoints(0, testService)
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

	endpoints(10, testService)
	a.Update(&DeciderSpec{
		TargetValue:         1,
		TotalValue:          1 / targetUtilization,
		TargetBurstCapacity: 71,
		PanicThreshold:      2,
		MaxScaleDownRate:    10,
		MaxScaleUpRate:      10,
		StableWindow:        stableWindow,
		ServiceName:         testService,
	})
	a.expectScale(t, time.Now(), 100, expectedEBC(1, 71, 100, 10), true)
}

type mockReporter struct{}

// ReportDesiredPodCount of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportDesiredPodCount(v int64) error {
	return nil
}

// ReportRequestedPodCount of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportRequestedPodCount(v int64) error {
	return nil
}

// ReportActualPodCount of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportActualPodCount(v int64) error {
	return nil
}

// ReportStableRequestConcurrency of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportStableRequestConcurrency(v float64) error {
	return nil
}

// ReportPanicRequestConcurrency of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportPanicRequestConcurrency(v float64) error {
	return nil
}

// ReportStableRPS of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportStableRPS(v float64) error {
	return nil
}

// ReportPanicRPS of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportPanicRPS(v float64) error {
	return nil
}

// ReportTargetRPS of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportTargetRPS(v float64) error {
	return nil
}

// ReportTargetRequestConcurrency of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportTargetRequestConcurrency(v float64) error {
	return nil
}

// ReportPanic of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportPanic(v int64) error {
	return nil
}

// ReportExcessBurstCapacity retports excess burst capacity.
func (r *mockReporter) ReportExcessBurstCapacity(v float64) error {
	return nil
}

func newTestAutoscaler(t *testing.T, targetValue, targetBurstCapacity float64, metrics MetricClient) *Autoscaler {
	return newTestAutoscalerWithScalingMetric(t, targetValue, targetBurstCapacity, metrics, "concurrency")
}

func newTestAutoscalerWithScalingMetric(t *testing.T, targetValue, targetBurstCapacity float64, metrics MetricClient, metric string) *Autoscaler {
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
		ServiceName:         testService,
	}

	l := kubeInformer.Core().V1().Endpoints().Lister()
	// This ensures that we have endpoints object to start the autoscaler.
	endpoints(0, testService)
	a, err := New(testNamespace, testRevision, metrics, l, deciderSpec, &mockReporter{})
	if err != nil {
		t.Fatalf("Error creating test autoscaler: %v", err)
	}
	endpoints(1, testService)
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

func endpoints(count int, svc string) {
	epAddresses := make([]corev1.EndpointAddress, count)
	for i := 0; i < count; i++ {
		ip := fmt.Sprintf("127.0.0.%v", i+1)
		epAddresses[i] = corev1.EndpointAddress{IP: ip}
	}

	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      svc,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: epAddresses,
		}},
	}
	kubeClient.CoreV1().Endpoints(testNamespace).Create(ep)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
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
		ServiceName:         testService,
	}

	l := kubeInformer.Core().V1().Endpoints().Lister()
	for i := 0; i < 2; i++ {
		endpoints(i, testService)
		a, err := New(testNamespace, testRevision, metrics, l, deciderSpec, &mockReporter{})
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
	endpoints(2, testService)
	a, err := New(testNamespace, testRevision, metrics, l, deciderSpec, &mockReporter{})
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
		ServiceName:         testService,
	}

	l := kubeInformer.Core().V1().Endpoints().Lister()
	a, err := New(testNamespace, testRevision, metrics, l, deciderSpec, &mockReporter{})
	if err != nil {
		t.Errorf("No endpoints should succeed, err = %v", err)
	}
	if got, want := int(a.maxPanicPods), 0; got != want {
		t.Errorf("maxPanicPods = %d, want: 0", got)
	}
}
