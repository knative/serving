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

package autoscaler_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/knative/pkg/logging/testing"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testRevision  = "test-revision"
	testNamespace = "test-namespace"
	testKPAKey    = "test-namespace/test-revision"
)

func TestMultiScalerScaling(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})
	defer close(stopCh)

	metric := newMetric()
	metricKey := fmt.Sprintf("%s/%s", metric.Namespace, metric.Name)

	uniScaler.setScaleResult(1, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !errors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	done := make(chan struct{})
	defer close(done)
	ms.Watch(func(key string) {
		// When we return, let the main process know.
		defer func() {
			done <- struct{}{}
		}()
		if key != metricKey {
			t.Errorf("Watch() = %v, wanted %v", key, metricKey)
		}
		m, err := ms.Get(ctx, metric.Namespace, metric.Name)
		if err != nil {
			t.Errorf("Get() = %v", err)
		}
		if got, want := m.Status.DesiredScale, int32(1); got != want {
			t.Errorf("Get() = %v, wanted %v", got, want)
		}
	})

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we see a "tick"
	select {
	case <-done:
		// We got the signal!
	case <-time.After(30 * time.Millisecond):
		t.Fatalf("timed out waiting for Watch()")
	}

	// Verify that subsequent "ticks" don't trigger a callback, since
	// the desired scale has not changed.
	select {
	case <-done:
		t.Fatalf("Got unexpected tick")
	case <-time.After(30 * time.Millisecond):
		// We got nothing!
	}

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	select {
	case <-done:
		t.Fatalf("Got unexpected tick")
	case <-time.After(30 * time.Millisecond):
		// We got nothing!
	}
}

func TestMultiScalerScaleToZero(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &autoscaler.Config{
		TickInterval:      time.Millisecond * 1,
		EnableScaleToZero: true,
	})
	defer close(stopCh)

	metric := newMetric()
	metricKey := fmt.Sprintf("%s/%s", metric.Namespace, metric.Name)

	uniScaler.setScaleResult(0, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !errors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	done := make(chan struct{})
	defer close(done)
	ms.Watch(func(key string) {
		// When we return, let the main process know.
		defer func() {
			done <- struct{}{}
		}()
		if key != metricKey {
			t.Errorf("Watch() = %v, wanted %v", key, metricKey)
		}
		m, err := ms.Get(ctx, metric.Namespace, metric.Name)
		if err != nil {
			t.Errorf("Get() = %v", err)
		}
		if got, want := m.Status.DesiredScale, int32(0); got != want {
			t.Errorf("Get() = %v, wanted %v", got, want)
		}
	})

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we see a "tick"
	select {
	case <-done:
		// We got the signal!
	case <-time.After(30 * time.Millisecond):
		t.Fatalf("timed out waiting for Watch()")
	}

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	select {
	case <-done:
		t.Fatalf("Got unexpected tick")
	case <-time.After(30 * time.Millisecond):
		// We got nothing!
	}
}

func TestMultiScalerWithoutScaleToZero(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &autoscaler.Config{
		TickInterval:      time.Millisecond * 1,
		EnableScaleToZero: false,
	})
	defer close(stopCh)

	metric := newMetric()

	uniScaler.setScaleResult(0, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !errors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	done := make(chan struct{})
	defer close(done)
	ms.Watch(func(key string) {
		// Let the main process know when this is called.
		done <- struct{}{}
	})

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we get no "ticks", because the desired scale is zero
	select {
	case <-done:
		t.Fatalf("Got unexpected tick")
	case <-time.After(30 * time.Millisecond):
		// We got nothing!
	}

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	select {
	case <-done:
		t.Fatalf("Got unexpected tick")
	case <-time.After(30 * time.Millisecond):
		// We got nothing!
	}
}

func TestMultiScalerIgnoreNegativeScale(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &autoscaler.Config{
		TickInterval:      time.Millisecond * 1,
		EnableScaleToZero: true,
	})
	defer close(stopCh)

	metric := newMetric()

	uniScaler.setScaleResult(-1, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !errors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	done := make(chan struct{})
	defer close(done)
	ms.Watch(func(key string) {
		// Let the main process know when this is called.
		done <- struct{}{}
	})

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we get no "ticks", because the desired scale is negative
	select {
	case <-done:
		t.Fatalf("Got unexpected tick")
	case <-time.After(30 * time.Millisecond):
		// We got nothing!
	}

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	select {
	case <-done:
		t.Fatalf("Got unexpected tick")
	case <-time.After(30 * time.Millisecond):
		// We got nothing!
	}
}

func TestMultiScalerRecordsStatistics(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})
	defer close(stopCh)

	metric := newMetric()

	uniScaler.setScaleResult(1, true)

	_, err := ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	now := time.Now()
	testStat := autoscaler.Stat{
		Time:                      &now,
		PodName:                   "test-pod",
		AverageConcurrentRequests: 3.5,
		RequestCount:              20,
	}

	ms.RecordStat(testKPAKey, testStat)
	uniScaler.checkLastStat(t, testStat)

	testStat.RequestCount = 10
	ms.RecordStat(testKPAKey, testStat)
	uniScaler.checkLastStat(t, testStat)

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Should not continue to record statistics after the KPA has been deleted.
	newStat := testStat
	newStat.RequestCount = 30
	ms.RecordStat(testKPAKey, newStat)
	uniScaler.checkLastStat(t, testStat)
}

func TestMultiScalerUpdate(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &autoscaler.Config{
		TickInterval:      time.Millisecond,
		EnableScaleToZero: false,
	})
	defer close(stopCh)

	metric := newMetric()
	metric.Spec.TargetConcurrency = 1.0
	uniScaler.setScaleResult(0, true)

	// Create the metric and verify the Spec
	_, err := ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if got, want := m.Spec.TargetConcurrency, 1.0; got != want {
		t.Errorf("Got target concurrency %v. Wanted %v", got, want)
	}

	// Update the target and verify the Spec
	metric.Spec.TargetConcurrency = 10.0
	if _, err = ms.Update(ctx, metric); err != nil {
		t.Errorf("Update() = %v", err)
	}
	m, err = ms.Get(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if got, want := m.Spec.TargetConcurrency, 10.0; got != want {
		t.Errorf("Got target concurrency %v. Wanted %v", got, want)
	}
}

func createMultiScaler(t *testing.T, config *autoscaler.Config) (*autoscaler.MultiScaler, chan<- struct{}, *fakeUniScaler) {
	logger := TestLogger(t)
	uniscaler := &fakeUniScaler{}

	stopChan := make(chan struct{})
	ms := autoscaler.NewMultiScaler(autoscaler.NewDynamicConfig(config, logger),
		stopChan, uniscaler.fakeUniScalerFactory, logger)

	return ms, stopChan, uniscaler
}

type fakeUniScaler struct {
	mutex    sync.Mutex
	replicas int32
	scaled   bool
	lastStat autoscaler.Stat
}

func (u *fakeUniScaler) fakeUniScalerFactory(*autoscaler.Metric, *autoscaler.DynamicConfig) (autoscaler.UniScaler, error) {
	return u, nil
}

func (u *fakeUniScaler) Scale(context.Context, time.Time) (int32, bool) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	return u.replicas, u.scaled
}

func (u *fakeUniScaler) setScaleResult(replicas int32, scaled bool) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	u.replicas = replicas
	u.scaled = scaled
}

func (u *fakeUniScaler) Record(ctx context.Context, stat autoscaler.Stat) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	u.lastStat = stat
}

func (u *fakeUniScaler) checkLastStat(t *testing.T, stat autoscaler.Stat) {
	t.Helper()

	if u.lastStat != stat {
		t.Fatalf("Last statistic recorded was %#v instead of expected statistic %#v", u.lastStat, stat)
	}
}

func (u *fakeUniScaler) Update(autoscaler.MetricSpec) error {
	return nil
}

type scaleParameterValues struct {
	kpa      *kpa.PodAutoscaler
	replicas int32
}

func newMetric() *autoscaler.Metric {
	return &autoscaler.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
		},
		Spec: autoscaler.MetricSpec{

			TargetConcurrency: 1,
		},
		Status: autoscaler.MetricStatus{},
	}
}
