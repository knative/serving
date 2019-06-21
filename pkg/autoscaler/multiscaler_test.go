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

	"k8s.io/apimachinery/pkg/util/wait"

	. "github.com/knative/pkg/logging/testing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	tickInterval = 5 * time.Millisecond
	tickTimeout  = 50 * time.Millisecond
)

// watchFunc generates a function to assert the changes happening in the multiscaler.
func watchFunc(ctx context.Context, ms *MultiScaler, decider *Decider, desiredScale int, errCh chan error) func(key string) {
	metricKey := fmt.Sprintf("%s/%s", decider.Namespace, decider.Name)
	return func(key string) {
		if key != metricKey {
			errCh <- fmt.Errorf("Watch() = %v, wanted %v", key, metricKey)
			return
		}
		m, err := ms.Get(ctx, decider.Namespace, decider.Name)
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
	ctx := context.Background()
	ms, stopCh, statCh, uniScaler := createMultiScaler(t)
	defer close(stopCh)
	defer close(statCh)

	decider := newDecider()
	uniScaler.setScaleResult(1, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, decider.Namespace, decider.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(watchFunc(ctx, ms, decider, 1, errCh))

	_, err = ms.Create(ctx, decider)
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

	if err := ms.Delete(ctx, decider.Namespace, decider.Name); err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerTickUpdate(t *testing.T) {
	ctx := context.Background()
	ms, stopCh, statCh, uniScaler := createMultiScaler(t)
	defer close(stopCh)
	defer close(statCh)

	decider := newDecider()
	decider.Spec.TickInterval = 10 * time.Second
	uniScaler.setScaleResult(1, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, decider.Namespace, decider.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	_, err = ms.Create(ctx, decider)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Expected count to be 0 as the tick interval is 10s and no autoscaling calculation should be triggered
	if count := uniScaler.getScaleCount(); count != 0 {
		t.Fatalf("Expected count to be 0 but got %d", count)
	}

	decider.Spec.TickInterval = time.Millisecond

	if _, err = ms.Update(ctx, decider); err != nil {
		t.Errorf("Update() = %v", err)
	}

	if err := wait.PollImmediate(time.Millisecond, 10*time.Millisecond, func() (bool, error) {
		// Expected count to be greater than 1 as the tick interval is updated to be 1ms
		if uniScaler.getScaleCount() >= 1 {
			return true, nil
		}
		return false, nil
	}); err != nil {
		t.Fatalf("Expected at least 1 tick but got %d", uniScaler.getScaleCount())
	}
}

func TestMultiScalerScaleToZero(t *testing.T) {
	ctx := context.Background()
	ms, stopCh, statCh, uniScaler := createMultiScaler(t)
	defer close(stopCh)
	defer close(statCh)

	decider := newDecider()
	uniScaler.setScaleResult(0, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, decider.Namespace, decider.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(watchFunc(ctx, ms, decider, 0, errCh))

	_, err = ms.Create(ctx, decider)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we see a "tick"
	if err := verifyTick(errCh); err != nil {
		t.Fatal(err)
	}

	err = ms.Delete(ctx, decider.Namespace, decider.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerScaleFromZero(t *testing.T) {
	ctx := context.Background()
	ms, stopCh, statCh, uniScaler := createMultiScaler(t)
	defer close(stopCh)
	defer close(statCh)

	decider := newDecider()
	decider.Spec.TickInterval = 60 * time.Second
	uniScaler.setScaleResult(1, true)

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(watchFunc(ctx, ms, decider, 1, errCh))

	_, err := ms.Create(ctx, decider)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}
	metricKey := NewMetricKey(decider.Namespace, decider.Name)
	if scaler, exists := ms.scalers[metricKey]; !exists {
		t.Errorf("Failed to get scaler for metric %s", metricKey)
	} else if !scaler.updateLatestScale(0) {
		t.Error("Failed to set scale for metric to 0")
	}

	now := time.Now()
	testStat := Stat{
		Time:                      &now,
		PodName:                   "test-pod",
		AverageConcurrentRequests: 1,
		RequestCount:              1,
	}
	ms.Poke(testKPAKey, testStat)

	// Verify that we see a "tick"
	if err := verifyTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerIgnoreNegativeScale(t *testing.T) {
	ctx := context.Background()
	ms, stopCh, statCh, uniScaler := createMultiScaler(t)
	defer close(stopCh)
	defer close(statCh)

	decider := newDecider()

	uniScaler.setScaleResult(-1, true)

	// Before it exists, we should get a NotFound.
	m, err := ms.Get(ctx, decider.Namespace, decider.Name)
	if !apierrors.IsNotFound(err) {
		t.Errorf("Get() = (%v, %v), want not found error", m, err)
	}

	errCh := make(chan error)
	defer close(errCh)
	ms.Watch(func(key string) {
		// Let the main process know when this is called.
		errCh <- nil
	})

	_, err = ms.Create(ctx, decider)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}

	// Verify that we get no "ticks", because the desired scale is negative
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}

	err = ms.Delete(ctx, decider.Namespace, decider.Name)
	if err != nil {
		t.Errorf("Delete() = %v", err)
	}

	// Verify that we stop seeing "ticks"
	if err := verifyNoTick(errCh); err != nil {
		t.Fatal(err)
	}
}

func TestMultiScalerUpdate(t *testing.T) {
	ctx := context.Background()
	ms, stopCh, statCh, uniScaler := createMultiScaler(t)
	defer close(stopCh)
	defer close(statCh)

	decider := newDecider()
	decider.Spec.TargetConcurrency = 1.0
	uniScaler.setScaleResult(0, true)

	// Create the decider and verify the Spec
	_, err := ms.Create(ctx, decider)
	if err != nil {
		t.Errorf("Create() = %v", err)
	}
	m, err := ms.Get(ctx, decider.Namespace, decider.Name)
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if got, want := m.Spec.TargetConcurrency, 1.0; got != want {
		t.Errorf("Got target concurrency %v. Wanted %v", got, want)
	}

	// Update the target and verify the Spec
	decider.Spec.TargetConcurrency = 10.0
	if _, err = ms.Update(ctx, decider); err != nil {
		t.Errorf("Update() = %v", err)
	}
	m, err = ms.Get(ctx, decider.Namespace, decider.Name)
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if got, want := m.Spec.TargetConcurrency, 10.0; got != want {
		t.Errorf("Got target concurrency %v. Wanted %v", got, want)
	}
}

func createMultiScaler(t *testing.T) (*MultiScaler, chan<- struct{}, chan *StatMessage, *fakeUniScaler) {
	logger := TestLogger(t)
	uniscaler := &fakeUniScaler{}

	stopChan := make(chan struct{})
	statChan := make(chan *StatMessage)
	ms := NewMultiScaler(stopChan, uniscaler.fakeUniScalerFactory, logger)

	return ms, stopChan, statChan, uniscaler
}

type fakeUniScaler struct {
	mutex      sync.RWMutex
	replicas   int32
	scaled     bool
	lastStat   Stat
	scaleCount int
}

func (u *fakeUniScaler) fakeUniScalerFactory(*Decider) (UniScaler, error) {
	return u, nil
}

func (u *fakeUniScaler) Scale(context.Context, time.Time) (int32, bool) {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	u.scaleCount++
	return u.replicas, u.scaled
}

func (u *fakeUniScaler) getScaleCount() int {
	u.mutex.RLock()
	defer u.mutex.RUnlock()
	return u.scaleCount
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

func (u *fakeUniScaler) Update(DeciderSpec) error {
	return nil
}

func newDecider() *Decider {
	return &Decider{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
		},
		Spec: DeciderSpec{
			TickInterval:      tickInterval,
			TargetConcurrency: 1,
		},
		Status: DeciderStatus{},
	}
}
