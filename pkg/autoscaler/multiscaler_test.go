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

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"go.uber.org/zap"
)

func TestMultiScalerScaling(t *testing.T) {
	ms, _, revisionScaler, uniScaler, logger := createMultiScaler(&autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(v1alpha1.RevisionServingStateActive)
	uniScaler.setScaleResult(1, true)

	ms.OnPresent(revision, logger)

	revisionScaler.checkScaleCall(t, 0, revision, 1)

	ms.OnAbsent(revision.Namespace, revision.Name, logger)

	revisionScaler.checkScaleNoLongerCalled(t)
}

func TestMultiScalerStop(t *testing.T) {
	ms, stopChan, revisionScaler, uniScaler, logger := createMultiScaler(&autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(v1alpha1.RevisionServingStateActive)
	uniScaler.setScaleResult(1, true)

	close(stopChan)

	ms.OnPresent(revision, logger)

	revisionScaler.checkScaleNoLongerCalled(t)

	ms.OnAbsent(revision.Namespace, revision.Name, logger)
}

func TestMultiScalerScaleToZeroWhenEnabled(t *testing.T) {
	ms, _, revisionScaler, uniScaler, logger := createMultiScaler(&autoscaler.Config{
		TickInterval:      time.Millisecond * 1,
		EnableScaleToZero: true,
	})

	revision := newRevision(v1alpha1.RevisionServingStateActive)
	uniScaler.setScaleResult(0, true)

	ms.OnPresent(revision, logger)

	revisionScaler.checkScaleCall(t, 0, revision, 0)

	ms.OnAbsent(revision.Namespace, revision.Name, logger)

	revisionScaler.checkScaleNoLongerCalled(t)
}

func TestMultiScalerDoesNotScaleToZeroWhenDisabled(t *testing.T) {
	ms, _, revisionScaler, uniScaler, logger := createMultiScaler(&autoscaler.Config{
		TickInterval:      time.Millisecond * 1,
		EnableScaleToZero: false,
	})

	revision := newRevision(v1alpha1.RevisionServingStateActive)
	uniScaler.setScaleResult(0, true)

	ms.OnPresent(revision, logger)

	revisionScaler.checkScaleNoLongerCalled(t)

	ms.OnAbsent(revision.Namespace, revision.Name, logger)
}

func TestMultiScalerIgnoresNegativeScales(t *testing.T) {
	ms, _, revisionScaler, uniScaler, logger := createMultiScaler(&autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(v1alpha1.RevisionServingStateActive)
	uniScaler.setScaleResult(-1, true)

	ms.OnPresent(revision, logger)

	revisionScaler.checkScaleNoLongerCalled(t)

	ms.OnAbsent(revision.Namespace, revision.Name, logger)
}

func TestMultiScalerRecordsStatistics(t *testing.T) {
	ms, _, _, uniScaler, logger := createMultiScaler(&autoscaler.Config{
		TickInterval: time.Millisecond * 1,
	})

	revision := newRevision(v1alpha1.RevisionServingStateActive)
	uniScaler.setScaleResult(1, true)

	ms.OnPresent(revision, logger)

	now := time.Now()
	testStat := autoscaler.Stat{
		Time:                      &now,
		PodName:                   "test-pod",
		AverageConcurrentRequests: 3.5,
		RequestCount:              20,
	}

	ms.RecordStat(testRevisionKey, testStat)
	uniScaler.checkLastStat(t, testStat)

	testStat.RequestCount = 10
	ms.RecordStat(testRevisionKey, testStat)
	uniScaler.checkLastStat(t, testStat)

	ms.OnAbsent(revision.Namespace, revision.Name, logger)

	// Should not continue to record statistics after the revision has been deleted.
	newStat := testStat
	newStat.RequestCount = 30
	ms.RecordStat(testRevisionKey, newStat)
	uniScaler.checkLastStat(t, testStat)
}

func createMultiScaler(config *autoscaler.Config) (*autoscaler.MultiScaler, chan<- struct{}, *fakeRevisionScaler, *fakeUniScaler, *zap.SugaredLogger) {
	logger := zap.NewNop().Sugar()
	revisionScaler := &fakeRevisionScaler{
		scaleChan: make(chan scaleParameterValues),
	}
	uniscaler := &fakeUniScaler{}

	stopChan := make(chan struct{})
	ms := autoscaler.NewMultiScaler(config, revisionScaler, stopChan, uniscaler.fakeUniScalerFactory, logger)

	return ms, stopChan, revisionScaler, uniscaler, logger
}

type fakeUniScaler struct {
	mutex    sync.Mutex
	replicas int32
	scaled   bool
	lastStat autoscaler.Stat
}

func (u *fakeUniScaler) fakeUniScalerFactory(*v1alpha1.Revision, *autoscaler.Config) (autoscaler.UniScaler, error) {
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
	revision *v1alpha1.Revision
	replicas int32
}

type fakeRevisionScaler struct {
	scaleParameters []scaleParameterValues
	scaleChan       chan scaleParameterValues
}

func (rs *fakeRevisionScaler) Scale(rev *v1alpha1.Revision, desiredScale int32) {
	rs.scaleChan <- scaleParameterValues{rev, desiredScale}
}

func (rs *fakeRevisionScaler) awaitScale(t *testing.T, n int) {
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

func (rs *fakeRevisionScaler) checkScaleCall(t *testing.T, index int, rev *v1alpha1.Revision, desiredReplicas int32) {
	t.Helper()

	rs.awaitScale(t, index+1)

	actualScaleParms := rs.scaleParameters[index]
	if actualScaleParms.revision != rev {
		t.Fatalf("Scale was called with unexpected revision %#v instead of expected revision %#v",
			actualScaleParms.revision, rev)
	}
	if actualScaleParms.replicas != desiredReplicas {
		t.Fatalf("Scale was called with unexpected replicas %d instead of expected replicas %d",
			actualScaleParms.replicas, desiredReplicas)
	}
}

func (rs *fakeRevisionScaler) checkScaleNoLongerCalled(t *testing.T) {
	t.Helper()

	select {
	case <-rs.scaleChan:
		t.Fatal("Scale was called unexpectedly")
	case <-time.After(time.Millisecond * 50):
		return
	}
}
