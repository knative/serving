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

package autoscaling_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/autoscaling"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestControllerSynchronizesCreatesAndDeletes(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	stopCh := make(chan struct{})
	createdCh := make(chan struct{})

	opts := controller.ReconcileOptions{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           zap.NewNop().Sugar(),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)

	fakeSynchronizer := newTestRevisionSynchronizer(createdCh, stopCh)
	ctl := autoscaling.NewController(&opts,
		servingInformer.Serving().V1alpha1().Revisions(),
		fakeSynchronizer,
		time.Duration(0), // disable resynch
	)

	servingInformer.Start(stopCh)

	rev := newTestRevision(testNamespace, testRevision)
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	ctl.Reconciler.Reconcile(testNamespace + "/" + testRevision)

	// Ensure revision creation has been seen before deleting it.
	select {
	case <-createdCh:
	case <-time.After(3 * time.Second):
		t.Fatal("Revision creation notification timed out")
	}

	if count := fakeSynchronizer.onPresentCallCount.Load(); count != 1 {
		t.Fatalf("OnPresent called %d times instead of once", count)
	}

	servingClient.ServingV1alpha1().Revisions(testNamespace).Delete(testRevision, nil)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Delete(rev)
	ctl.Reconciler.Reconcile(testNamespace + "/" + testRevision)

	if fakeSynchronizer.onAbsentCallCount.Load() == 0 {
		t.Fatal("OnAbsent was not called")
	}

	if fakeSynchronizer.absentRanBeforePresent.Load() {
		t.Fatal("OnAbsent ran before OnPresent")
	}
}

func newTestRevisionSynchronizer(createdCh chan struct{}, stopCh chan struct{}) *testRevisionSynchronizer {
	return &testRevisionSynchronizer{atomic.NewUint32(0), atomic.NewUint32(0), atomic.NewBool(false), createdCh, stopCh}
}

type testRevisionSynchronizer struct {
	onPresentCallCount     *atomic.Uint32
	onAbsentCallCount      *atomic.Uint32
	absentRanBeforePresent *atomic.Bool
	createdCh              chan struct{}
	stopCh                 chan struct{}
}

func (revSynch *testRevisionSynchronizer) OnPresent(rev *v1alpha1.Revision, logger *zap.SugaredLogger) {
	revSynch.onPresentCallCount.Add(1)
	close(revSynch.createdCh)
}

func (revSynch *testRevisionSynchronizer) OnAbsent(namespace string, name string, logger *zap.SugaredLogger) {
	revSynch.onAbsentCallCount.Add(1)
	if revSynch.onPresentCallCount.Load() > 0 {
		// OnAbsent may be called more than once
		if revSynch.onAbsentCallCount.Load() == 1 {
			close(revSynch.stopCh)
		}
	} else {
		revSynch.absentRanBeforePresent.Store(true)
	}
}

func newTestRevision(namespace string, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1alpha1/namespaces/%s/revisions/%s", namespace, name),
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.RevisionSpec{
			Container: corev1.Container{
				Image:      "gcr.io/repo/image",
				Command:    []string{"echo"},
				Args:       []string{"hello", "world"},
				WorkingDir: "/tmp",
			},
			ConcurrencyModel: v1alpha1.RevisionRequestConcurrencyModelSingle,
		},
	}
}
