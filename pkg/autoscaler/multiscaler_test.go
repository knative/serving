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
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/knative/pkg/logging/testing"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	tickInterval = 5 * time.Millisecond
	tickTimeout  = 50 * time.Millisecond
)

// watchFunc generates a function to assert the changes happening in the multiscaler.
func watchFunc(ctx context.Context, ms *MultiScaler, metric *Metric, desiredScale int, errCh chan error) func(key string) {
	metricKey := fmt.Sprintf("%s/%s", metric.Namespace, metric.Name)
	return func(key string) {
		if key != metricKey {
			errCh <- fmt.Errorf("Watch() = %v, wanted %v", key, metricKey)
			return
		}
		m, err := ms.Get(ctx, metric.Namespace, metric.Name)
		if err != nil {
			errCh <- fmt.Errorf("Get() = %v", err)
			return
		}
		if got, want := m.Status.DesiredScale, int32(desiredScale); got != want {
			errCh <- fmt.Errorf("Get() = %v, wanted %v", got, want)
			return
		}
		errCh <- nil
	}
}

// verifyTick verifies that we get a tick in a certain amount of time.
func verifyTick(errCh chan error) error {
	select {
	case err := <-errCh:
		return err
	case <-time.After(tickTimeout):
		return errors.New("timed out waiting for Watch()")
	}
}

// verifyNoTick verifies that we don't get a tick in a certain amount of time.
func verifyNoTick(errCh chan error) error {
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
		return errors.New("Got unexpected tick")
	case <-time.After(tickTimeout):
		// Got nothing, all good!
		return nil
	}
}

func TestMultiScalerScaling(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &Config{
		TickInterval: tickInterval,
	})
	defer close(stopCh)

	metric := newMetric()
	uniScaler.setScaleResult(1, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(watchFunc(ctx, ms, metric, 1, errCh))

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we see a "tick"
	if err := verifyTick(errCh); err != nil {
		t.Fatal(err)
	}

	// Verify that subsequent "ticks" don't trigger a callback, since
	// the desired scale has not changed.
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}

	if err := ms.Delete(ctx, metric.Namespace, metric.Name); err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerScaleToZero(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &Config{
		TickInterval:      tickInterval,
		EnableScaleToZero: true,
	})
	defer close(stopCh)

	metric := newMetric()
	uniScaler.setScaleResult(0, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(watchFunc(ctx, ms, metric, 0, errCh))

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we see a "tick"
	if err := verifyTick(errCh); err != nil {
		t.Fatal(err)
	}

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerScaleFromZero(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &Config{
		TickInterval:      time.Second * 60,
		EnableScaleToZero: true,
	})
	defer close(stopCh)

	metric := newMetric()
	uniScaler.setScaleResult(1, true)

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(watchFunc(ctx, ms, metric, 1, errCh))

	_, err := ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}
	if ok := ms.setScale(NewMetricKey(metric.Namespace, metric.Name), 0); !ok {
		t.Error("Failed to set scale for metric to 0")
	}

	now := time.Now()
	testStat := Stat{
		Time:                      &now,
		PodName:                   "test-pod",
		AverageConcurrentRequests: 1,
		RequestCount:              1,
	}
	ms.RecordStat(testKPAKey, testStat)

	// Verify that we see a "tick"
	if err := verifyTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerWithoutScaleToZero(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &Config{
		TickInterval:      tickInterval,
		EnableScaleToZero: false,
	})
	defer close(stopCh)

	metric := newMetric()
	uniScaler.setScaleResult(0, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(func(key string) {
		// Let the main process know when this is called.
		errCh <- nil
	})

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we get no "ticks", because the desired scale is zero
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerIgnoreNegativeScale(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &Config{
		TickInterval:      tickInterval,
		EnableScaleToZero: true,
	})
	defer close(stopCh)

	metric := newMetric()

	uniScaler.setScaleResult(-1, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, metric.Namespace, metric.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(func(key string) {
		// Let the main process know when this is called.
		errCh <- nil
	})

	_, err = ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we get no "ticks", because the desired scale is negative
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}

	err = ms.Delete(ctx, metric.Namespace, metric.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerRecordsStatistics(t *testing.T) {
	ctx := context.TODO()
	ms, stopCh, uniScaler := createMultiScaler(t, &Config{
		TickInterval: tickInterval,
	})
	defer close(stopCh)

	metric := newMetric()

	uniScaler.setScaleResult(1, true)

	_, err := ms.Create(ctx, metric)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	now := time.Now()
	testStat := Stat{
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
	ms, stopCh, uniScaler := createMultiScaler(t, &Config{
		TickInterval:      tickInterval,
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

func createMultiScaler(t *testing.T, config *Config) (*MultiScaler, chan<- struct{}, *fakeUniScaler) {
	logger := TestLogger(t)
	uniscaler := &fakeUniScaler{}

	stopChan := make(chan struct{})
	ms := NewMultiScaler(NewDynamicConfig(config, logger),
		stopChan, uniscaler.fakeUniScalerFactory, logger)

	return ms, stopChan, uniscaler
}

type fakeUniScaler struct {
	mutex    sync.Mutex
	replicas int32
	scaled   bool
	lastStat Stat
}

func (u *fakeUniScaler) fakeUniScalerFactory(*Metric, *DynamicConfig) (UniScaler, error) {
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

func (u *fakeUniScaler) Record(ctx context.Context, stat Stat) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	u.lastStat = stat
}

func (u *fakeUniScaler) checkLastStat(t *testing.T, stat Stat) {
	t.Helper()

	if u.lastStat != stat {
		t.Fatalf("Last statistic recorded was %#v instead of expected statistic %#v", u.lastStat, stat)
	}
}

func (u *fakeUniScaler) Update(MetricSpec) error {
	return nil
}

type scaleParameterValues struct {
	kpa      *kpa.PodAutoscaler
	replicas int32
}

func newMetric() *Metric {
	return &Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
		},
		Spec: MetricSpec{

			TargetConcurrency: 1,
		},
		Status: MetricStatus{},
	}
}
