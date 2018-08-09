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
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"

	. "github.com/knative/pkg/logging/testing"
)

const (
	testRevisionKey = "test-namespace/test-revision"
	testKPAKey      = "test-namespace/test-revision"
)

func TestMultiScalerScaling(t *testing.T) {
	servingClient := fakeKna.NewSimpleClientset()
	ms, _, kpaScaler, uniScaler, logger := createMultiScaler(t, &autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(t, servingClient, v1alpha1.RevisionServingStateActive)
	kpa := newKPA(t, servingClient, revision)
	uniScaler.setScaleResult(1, true)

	ms.OnPresent(kpa, logger)

	kpaScaler.checkScaleCall(t, 0, kpa, 1)

	ms.OnAbsent(kpa.Namespace, kpa.Name, logger)

	kpaScaler.checkScaleNoLongerCalled(t)
}

func TestMultiScalerStop(t *testing.T) {
	servingClient := fakeKna.NewSimpleClientset()
	ms, stopChan, kpaScaler, uniScaler, logger := createMultiScaler(t, &autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(t, servingClient, v1alpha1.RevisionServingStateActive)
	kpa := newKPA(t, servingClient, revision)
	uniScaler.setScaleResult(1, true)

	close(stopChan)

	ms.OnPresent(kpa, logger)

	kpaScaler.checkScaleNoLongerCalled(t)

	ms.OnAbsent(kpa.Namespace, kpa.Name, logger)
}

func TestMultiScalerScaleToZeroWhenEnabled(t *testing.T) {
	servingClient := fakeKna.NewSimpleClientset()
	ms, _, kpaScaler, uniScaler, logger := createMultiScaler(t, &autoscaler.Config{
		TickInterval:      time.Millisecond * 1,
		EnableScaleToZero: true,
	})

	revision := newRevision(t, servingClient, v1alpha1.RevisionServingStateActive)
	kpa := newKPA(t, servingClient, revision)
	uniScaler.setScaleResult(0, true)

	ms.OnPresent(kpa, logger)

	kpaScaler.checkScaleCall(t, 0, kpa, 0)

	ms.OnAbsent(kpa.Namespace, kpa.Name, logger)

	kpaScaler.checkScaleNoLongerCalled(t)
}

func TestMultiScalerDoesNotScaleToZeroWhenDisabled(t *testing.T) {
	servingClient := fakeKna.NewSimpleClientset()
	ms, _, kpaScaler, uniScaler, logger := createMultiScaler(t, &autoscaler.Config{
		TickInterval:      time.Millisecond * 1,
		EnableScaleToZero: false,
	})

	revision := newRevision(t, servingClient, v1alpha1.RevisionServingStateActive)
	kpa := newKPA(t, servingClient, revision)
	uniScaler.setScaleResult(0, true)

	ms.OnPresent(kpa, logger)

	kpaScaler.checkScaleNoLongerCalled(t)

	ms.OnAbsent(kpa.Namespace, kpa.Name, logger)
}

func TestMultiScalerIgnoresNegativeScales(t *testing.T) {
	servingClient := fakeKna.NewSimpleClientset()
	ms, _, kpaScaler, uniScaler, logger := createMultiScaler(t, &autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(t, servingClient, v1alpha1.RevisionServingStateActive)
	kpa := newKPA(t, servingClient, revision)
	uniScaler.setScaleResult(-1, true)

	ms.OnPresent(kpa, logger)

	kpaScaler.checkScaleNoLongerCalled(t)

	ms.OnAbsent(kpa.Namespace, kpa.Name, logger)
}

func TestMultiScalerRecordsStatistics(t *testing.T) {
	servingClient := fakeKna.NewSimpleClientset()
	ms, _, _, uniScaler, logger := createMultiScaler(t, &autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(t, servingClient, v1alpha1.RevisionServingStateActive)
	kpa := newKPA(t, servingClient, revision)
	uniScaler.setScaleResult(1, true)

	ms.OnPresent(kpa, logger)

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

	ms.OnAbsent(kpa.Namespace, kpa.Name, logger)

	// Should not continue to record statistics after the KPA has been deleted.
	newStat := testStat
	newStat.RequestCount = 30
	ms.RecordStat(testKPAKey, newStat)
	uniScaler.checkLastStat(t, testStat)
}

func createMultiScaler(t *testing.T, config *autoscaler.Config) (*autoscaler.MultiScaler, chan<- struct{}, *fakeKPAScaler, *fakeUniScaler, *zap.SugaredLogger) {
	logger := TestLogger(t)
	kpaScaler := &fakeKPAScaler{
		scaleChan: make(chan scaleParameterValues),
	}
	uniscaler := &fakeUniScaler{}

	stopChan := make(chan struct{})
	ms := autoscaler.NewMultiScaler(autoscaler.NewDynamicConfig(config, logger),
		kpaScaler, stopChan, uniscaler.fakeUniScalerFactory, logger)

	return ms, stopChan, kpaScaler, uniscaler, logger
}

type fakeUniScaler struct {
	mutex    sync.Mutex
	replicas int32
	scaled   bool
	lastStat autoscaler.Stat
}

func (u *fakeUniScaler) fakeUniScalerFactory(*kpa.PodAutoscaler, *autoscaler.DynamicConfig) (autoscaler.UniScaler, error) {
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

type scaleParameterValues struct {
	kpa      *kpa.PodAutoscaler
	replicas int32
}

type fakeKPAScaler struct {
	scaleParameters []scaleParameterValues
	scaleChan       chan scaleParameterValues
}

func (rs *fakeKPAScaler) Scale(kpa *kpa.PodAutoscaler, desiredScale int32) {
	rs.scaleChan <- scaleParameterValues{kpa, desiredScale}
}

func (rs *fakeKPAScaler) awaitScale(t *testing.T, n int) {
	t.Helper()

	for {
		if len(rs.scaleParameters) >= n {
			return
		}
		select {
		case pv := <-rs.scaleChan:
			rs.scaleParameters = append(rs.scaleParameters, pv)
		case <-time.After(time.Second * 10):
			t.Fatalf("Timed out waiting for Scale to be called at least %d times", n)
		}
	}
}

func (rs *fakeKPAScaler) checkScaleCall(t *testing.T, index int, kpa *kpa.PodAutoscaler, desiredReplicas int32) {
	t.Helper()

	rs.awaitScale(t, index+1)

	actualScaleParms := rs.scaleParameters[index]
	if actualScaleParms.kpa != kpa {
		t.Fatalf("Scale was called with unexpected KPA %#v instead of expected KPA %#v",
			actualScaleParms.kpa, kpa)
	}
	if actualScaleParms.replicas != desiredReplicas {
		t.Fatalf("Scale was called with unexpected replicas %d instead of expected replicas %d",
			actualScaleParms.replicas, desiredReplicas)
	}
}

func (rs *fakeKPAScaler) checkScaleNoLongerCalled(t *testing.T) {
	t.Helper()

	select {
	case <-rs.scaleChan:
		t.Fatal("Scale was called unexpectedly")
	case <-time.After(time.Millisecond * 50):
		return
	}
}
