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
	"testing"
	"time"

	"github.com/knative/serving/pkg/resources"

	. "github.com/knative/pkg/logging/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
)

const stableWindow = 60 * time.Second

var (
	kubeClient   = fakeK8s.NewSimpleClientset()
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
)

func TestNewErrorWhenGivenNilReadyPodCounter(t *testing.T) {
	_, err := New(testNamespace, testRevision, &testMetricClient{}, nil, DeciderSpec{TargetConcurrency: 10, ServiceName: testService}, &mockReporter{})
	if err == nil {
		t.Error("Expected error when ReadyPodCounter interface is nil, but got none.")
	}
}

func TestNewErrorWhenGivenNilStatsReporter(t *testing.T) {
	var reporter StatsReporter

	podCounter := resources.NewScopedEndpointsCounter(kubeInformer.Core().V1().Endpoints().Lister(), testNamespace, testService)

	_, err := New(testNamespace, testRevision, &testMetricClient{}, podCounter,
		DeciderSpec{TargetConcurrency: 10, ServiceName: testService}, reporter)
	if err == nil {
		t.Error("Expected error when EndpointsInformer interface is nil, but got none.")
	}
}

func TestAutoscalerNoDataNoAutoscale(t *testing.T) {
	defer ClearAll()
	metrics := &testMetricClient{
		err: errors.New("no metrics"),
	}

	a := newTestAutoscaler(10.0, metrics)
	a.expectScale(t, time.Now(), 0, false)
}

func TestAutoscalerNoDataAtZeroNoAutoscale(t *testing.T) {
	a := newTestAutoscaler(10.0, &testMetricClient{})
	a.expectScale(t, time.Now(), 0, true)
}

func TestAutoscalerStableModeNoChange(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 50.0}
	a := newTestAutoscaler(10.0, metrics)
	a.expectScale(t, time.Now(), 5, true)
}

func TestAutoscalerStableModeIncrease(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 50.0}
	a := newTestAutoscaler(10.0, metrics)
	a.expectScale(t, time.Now(), 5, true)

	metrics.stableConcurrency = 100.0
	a.expectScale(t, time.Now(), 10, true)
}

func TestAutoscalerStableModeDecrease(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 100.0}
	a := newTestAutoscaler(10.0, metrics)
	a.expectScale(t, time.Now(), 10, true)

	metrics.stableConcurrency = 50.0
	a.expectScale(t, time.Now(), 5, true)
}

func TestAutoscaler_StableModeNoTrafficScaleToZero(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 1.0}
	a := newTestAutoscaler(10.0, metrics)
	a.expectScale(t, time.Now(), 1, true)

	metrics.stableConcurrency = 0.0
	a.expectScale(t, time.Now(), 0, true)
}

func TestAutoscalerPanicModeDoublePodCount(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 50.0, panicConcurrency: 100.0}
	a := newTestAutoscaler(10.0, metrics)

	// PanicConcurrency takes precedence.
	a.expectScale(t, time.Now(), 10, true)
}

// QPS is increasing exponentially. Each scaling event bring concurrency
// back to the target level (1.0) but then traffic continues to increase.
// At 1296 QPS traffic stablizes.
func TestAutoscalerPanicModeExponentialTrackAndStablize(t *testing.T) {
	metrics := &testMetricClient{panicConcurrency: 6.0}
	a := newTestAutoscaler(1.0, metrics)
	a.expectScale(t, time.Now(), 6, true)
	endpoints(6)

	metrics.panicConcurrency = 36.0
	a.expectScale(t, time.Now(), 36, true)
	endpoints(36)

	metrics.panicConcurrency = 216.0
	a.expectScale(t, time.Now(), 216, true)
	endpoints(216)

	metrics.panicConcurrency = 1296.0
	a.expectScale(t, time.Now(), 1296, true)
	endpoints(1296)
}

func TestAutoscalerPanicThenUnPanicScaleDown(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 100.0, panicConcurrency: 100.0}
	a := newTestAutoscaler(10.0, metrics)
	a.expectScale(t, time.Now(), 10, true)
	endpoints(10)

	panicTime := time.Now()
	metrics.panicConcurrency = 1000.0
	a.expectScale(t, panicTime, 100, true)

	// Traffic dropped off, scale stays as we're still in panic.
	metrics.panicConcurrency = 1.0
	metrics.stableConcurrency = 1.0
	a.expectScale(t, panicTime.Add(30*time.Second), 100, true)

	// Scale down after the StableWindow
	a.expectScale(t, panicTime.Add(61*time.Second), 1, true)
}

func TestAutoscalerRateLimitScaleUp(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 1000.0}
	a := newTestAutoscaler(10.0, metrics)
	endpoints(1)

	// Need 100 pods but only scale x10
	a.expectScale(t, time.Now(), 10, true)

	endpoints(10)
	// Scale x10 again
	a.expectScale(t, time.Now(), 100, true)
}

func TestAutoscalerUseOnePodAsMinimumIfEndpointsNotFound(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 1000.0}
	a := newTestAutoscaler(10.0, metrics)

	endpoints(0)
	// 2*10 as the rate limited if we can get the actual pods number.
	// 1*10 as the rate limited since no read pods are there from K8S API.
	a.expectScale(t, time.Now(), 10, true)

	ep, _ := kubeClient.CoreV1().Endpoints(testNamespace).Get(testService, metav1.GetOptions{})
	kubeClient.CoreV1().Endpoints(testNamespace).Delete(testService, nil)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Delete(ep)
	// 2*10 as the rate limited if we can get the actual pods number.
	// 1*10 as the rate limited since no Endpoints object is there from K8S API.
	a.expectScale(t, time.Now(), 10, true)
}

func TestAutoscalerUpdateTarget(t *testing.T) {
	metrics := &testMetricClient{stableConcurrency: 100.0}
	a := newTestAutoscaler(10.0, metrics)
	a.expectScale(t, time.Now(), 10, true)
	endpoints(10)

	a.Update(DeciderSpec{
		TargetConcurrency: 1.0,
		PanicThreshold:    2.0,
		MaxScaleUpRate:    10.0,
		StableWindow:      stableWindow,
		ServiceName:       testService,
	})
	a.expectScale(t, time.Now(), 100, true)
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

// ReportTargetRequestConcurrency of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportTargetRequestConcurrency(v float64) error {
	return nil
}

// ReportPanic of a mockReporter does nothing and return nil for error.
func (r *mockReporter) ReportPanic(v int64) error {
	return nil
}

func newTestAutoscaler(containerConcurrency int, metrics MetricClient) *Autoscaler {
	deciderSpec := DeciderSpec{
		TargetConcurrency: float64(containerConcurrency),
		PanicThreshold:    2 * float64(containerConcurrency),
		MaxScaleUpRate:    10.0,
		StableWindow:      stableWindow,
		ServiceName:       testService,
	}

	podCounter := resources.NewScopedEndpointsCounter(kubeInformer.Core().V1().Endpoints().Lister(), testNamespace, deciderSpec.ServiceName)
	a, _ := New(testNamespace, testRevision, metrics, podCounter, deciderSpec, &mockReporter{})
	return a
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

type testMetricClient struct {
	stableConcurrency float64
	panicConcurrency  float64
	err               error
}

func (t *testMetricClient) StableAndPanicConcurrency(key string) (float64, float64, error) {
	return t.stableConcurrency, t.panicConcurrency, t.err
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
