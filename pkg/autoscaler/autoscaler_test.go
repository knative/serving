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
	"knative.dev/serving/pkg/resources"
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
	_, err := New(testNamespace, testRevision, &autoscalerfake.MetricClient{}, nil, DeciderSpec{TargetValue: 10, ServiceName: testService}, &mockReporter{})
	if err == nil {
		t.Error("Expected error when ReadyPodCounter interface is nil, but got none.")
	}
}

func TestNewErrorWhenGivenNilStatsReporter(t *testing.T) {
	var reporter StatsReporter

	podCounter := resources.NewScopedEndpointsCounter(kubeInformer.Core().V1().Endpoints().Lister(), testNamespace, testService)

	_, err := New(testNamespace, testRevision, &autoscalerfake.MetricClient{}, podCounter,
		DeciderSpec{TargetValue: 10, ServiceName: testService}, reporter)
	if err == nil {
		t.Error("Expected error when EndpointsInformer interface is nil, but got none.")
	}
}

func TestAutoscalerNoDataNoAutoscale(t *testing.T) {
	defer ClearAll()
	metrics := &autoscalerfake.MetricClient{
		ErrF: func(key types.NamespacedName, now time.Time) error {
			return errors.New("no metrics")
		},
	}

	a := newTestAutoscaler(t, 10, 100, metrics)
	a.expectScale(t, time.Now(), 0, 0, false)
}

func expectedEBC(tc, tbc, rc, np float64) int32 {
	return int32(math.Floor(tc/targetUtilization*np - tbc - rc))
}
func TestAutoscalerNoDataAtZeroNoAutoscale(t *testing.T) {
	a := newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{})
	// We always presume at least 1 pod, even if counter says 0.
	a.expectScale(t, time.Now(), 0, expectedEBC(10, 100, 0, 1), true)
}

func TestAutoscalerNoDataAtZeroNoAutoscaleWithExplicitEPs(t *testing.T) {
	a := newTestAutoscaler(t, 10, 100, &autoscalerfake.MetricClient{})
	endpoints(1)
	a.expectScale(t, time.Now(), 0, expectedEBC(10, 100, 0, 1), true)
}

func TestAutoscalerStableModeUnlimitedTBC(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 21.0}
	a := newTestAutoscaler(t, 181, -1, metrics)
	a.expectScale(t, time.Now(), 1, -1, true)
}

func TestAutoscaler0TBC(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 50.0}
	a := newTestAutoscaler(t, 10, 0, metrics)
	a.expectScale(t, time.Now(), 5, 0, true)
}

func TestAutoscalerStableModeNoChange(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 50.0}
	a := newTestAutoscaler(t, 10, 100, metrics)
	a.expectScale(t, time.Now(), 5, expectedEBC(10, 100, 50, 1), true)
}

func TestAutoscalerStableModeNoChangeAlreadyScaled(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 50.0}
	a := newTestAutoscaler(t, 10, 100, metrics)
	endpoints(5)
	a.expectScale(t, time.Now(), 5, expectedEBC(10, 100, 50, 5), true)
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
	endpoints(8)
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

func TestAutoscalerPanicModeDoublePodCount(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 50, PanicConcurrency: 100}
	a := newTestAutoscaler(t, 10, 84, metrics)

	// PanicConcurrency takes precedence.
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 84, 50, 1), true)
}

// QPS is increasing exponentially. Each scaling event bring concurrency
// back to the target level (1.0) but then traffic continues to increase.
// At 1296 QPS traffic stablizes.
func TestAutoscalerPanicModeExponentialTrackAndStablize(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 6, PanicConcurrency: 6}
	a := newTestAutoscaler(t, 1, 101, metrics)
	a.expectScale(t, time.Now(), 6, expectedEBC(1, 101, 6, 1), true)

	endpoints(6)
	metrics.PanicConcurrency, metrics.StableConcurrency = 36, 36
	a.expectScale(t, time.Now(), 36, expectedEBC(1, 101, 36, 6), true)

	endpoints(36)
	metrics.PanicConcurrency, metrics.StableConcurrency = 216, 216
	a.expectScale(t, time.Now(), 216, expectedEBC(1, 101, 216, 36), true)

	endpoints(216)
	metrics.PanicConcurrency, metrics.StableConcurrency = 1296, 1296
	a.expectScale(t, time.Now(), 1296, expectedEBC(1, 101, 1296, 216), true)
	endpoints(1296)
	a.expectScale(t, time.Now(), 1296, expectedEBC(1, 101, 1296, 1296), true)
}

func TestAutoscalerPanicThenUnPanicScaleDown(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 100, PanicConcurrency: 100}
	a := newTestAutoscaler(t, 10, 93, metrics)
	a.expectScale(t, time.Now(), 10, expectedEBC(10, 93, 100, 1), true)
	endpoints(10)

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

	endpoints(10)
	// Scale x10 again
	a.expectScale(t, time.Now(), 100, expectedEBC(10, 61, 1000, 10), true)
}

func eraseEndpoints() {
	ep, _ := kubeClient.CoreV1().Endpoints(testNamespace).Get(testService, metav1.GetOptions{})
	kubeClient.CoreV1().Endpoints(testNamespace).Delete(testService, nil)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Delete(ep)
}

func TestAutoscalerUseOnePodAsMinimumIfEndpointsNotFound(t *testing.T) {
	metrics := &autoscalerfake.MetricClient{StableConcurrency: 1000}
	a := newTestAutoscaler(t, 10, 81, metrics)

	endpoints(0)
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

	endpoints(10)
	a.Update(DeciderSpec{
		TargetValue:         1,
		TotalValue:          1 / targetUtilization,
		TargetBurstCapacity: 71,
		PanicThreshold:      2,
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
	deciderSpec := DeciderSpec{
		ScalingMetric:       metric,
		TargetValue:         targetValue,
		TotalValue:          targetValue / targetUtilization, // For UTs presume 75% utilization
		TargetBurstCapacity: targetBurstCapacity,
		PanicThreshold:      2 * targetValue,
		MaxScaleUpRate:      10.0,
		StableWindow:        stableWindow,
		ServiceName:         testService,
	}

	podCounter := resources.NewScopedEndpointsCounter(kubeInformer.Core().V1().Endpoints().Lister(), testNamespace, deciderSpec.ServiceName)
	// This ensures that we have endpoints object to start the autoscaler.
	endpoints(0)
	a, err := New(testNamespace, testRevision, metrics, podCounter, deciderSpec, &mockReporter{})
	if err != nil {
		t.Fatalf("Error creating test autoscaler: %v", err)
	}
	endpoints(1)
	return a
}

func (a *Autoscaler) expectScale(t *testing.T, now time.Time, expectScale, expectEBC int32, expectOk bool) {
	t.Helper()
	scale, ebc, ok := a.Scale(TestContextWithLogger(t), now)
	if ok != expectOk {
		t.Errorf("Unexpected autoscale decision. Expected %v. Got %v.", expectOk, ok)
	}
	if got, want := scale, expectScale; got != want {
		t.Errorf("Scale %d, want: %d", got, want)
	}
	if got, want := ebc, expectEBC; got != want {
		t.Errorf("ExcessBurstCapacity = %d, want: %d", got, want)
	}
}

func endpoints(count int) {
	epAddresses := make([]corev1.EndpointAddress, count)
	for i := 0; i < count; i++ {
		ip := fmt.Sprintf("127.0.0.%v", i+1)
		epAddresses[i] = corev1.EndpointAddress{IP: ip}
	}

	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testService,
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
	deciderSpec := DeciderSpec{
		TargetValue:         100,
		TotalValue:          120,
		TargetBurstCapacity: 11,
		PanicThreshold:      220,
		MaxScaleUpRate:      10.0,
		StableWindow:        stableWindow,
		ServiceName:         testService,
	}

	podCounter := resources.NewScopedEndpointsCounter(kubeInformer.Core().V1().Endpoints().Lister(), testNamespace, deciderSpec.ServiceName)
	for i := 0; i < 2; i++ {
		endpoints(i)
		a, err := New(testNamespace, testRevision, metrics, podCounter, deciderSpec, &mockReporter{})
		if err != nil {
			t.Fatalf("Error creating test autoscaler: %v", err)
		}
		if a.panicTime != nil {
			t.Errorf("Create at scale %d had panic mode on", i)
		}
		if got, want := int(a.maxPanicPods), i; got != want {
			t.Errorf("MaxPanicPods = %d, want: %d", got, want)
		}
	}

	// Now start with 2 and make sure we're in panic mode.
	endpoints(2)
	a, err := New(testNamespace, testRevision, metrics, podCounter, deciderSpec, &mockReporter{})
	if err != nil {
		t.Fatalf("Error creating test autoscaler: %v", err)
	}
	if a.panicTime == nil {
		t.Error("Create at scale 2 had panic mode off")
	}
	if got, want := int(a.maxPanicPods), 2; got != want {
		t.Errorf("MaxPanicPods = %d, want: %d", got, want)
	}
}

func TestNewFail(t *testing.T) {
	eraseEndpoints()
	metrics := &autoscalerfake.StaticMetricClient
	deciderSpec := DeciderSpec{
		TargetValue:         100,
		TotalValue:          120,
		TargetBurstCapacity: 11,
		PanicThreshold:      220,
		MaxScaleUpRate:      10.0,
		StableWindow:        stableWindow,
		ServiceName:         testService,
	}

	podCounter := resources.NewScopedEndpointsCounter(kubeInformer.Core().V1().Endpoints().Lister(), testNamespace, deciderSpec.ServiceName)
	a, err := New(testNamespace, testRevision, metrics, podCounter, deciderSpec, &mockReporter{})
	if err != nil {
		t.Errorf("No endpoints should succeed, err = %v", err)
	}
	if got, want := int(a.maxPanicPods), 0; got != want {
		t.Errorf("maxPanicPods = %d, want: 0", got)
	}
}
