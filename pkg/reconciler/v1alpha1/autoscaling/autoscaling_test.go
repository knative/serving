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

package autoscaling

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/knative/pkg/configmap"
	. "github.com/knative/pkg/logging/testing"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	"github.com/knative/serving/pkg/system"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
	scalefake "k8s.io/client-go/scale/fake"
)

var (
	gracePeriod = 60 * time.Second
)

func newConfigWatcher() configmap.Watcher {
	return configmap.NewStaticWatcher(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      autoscaler.ConfigName,
			},
			Data: map[string]string{
				"max-scale-up-rate":                       "1.0",
				"container-concurrency-target-percentage": "0.5",
				"container-concurrency-target-default":    "10.0",
				"stable-window":                           "5m",
				"panic-window":                            "10s",
				"scale-to-zero-threshold":                 "10m",
				"scale-to-zero-grace-period":              gracePeriod.String(),
				"tick-interval":                           "2s",
			},
		})
}

func TestCreateAndDelete(t *testing.T) {

	cases := []struct {
		name          string
		annotations   map[string]string
		wantKpaCreate bool
		wantHpaCreate bool
		wantKpaDelete bool
		wantHpaDelete bool
	}{{
		name:          "default class (kpa)",
		annotations:   map[string]string{},
		wantKpaCreate: true,
		wantHpaCreate: false,
		wantKpaDelete: false,
		wantHpaDelete: true,
	}, {
		name: "hpa class",
		annotations: map[string]string{
			"autoscaling.knative.dev/class": "hpa",
		},
		wantKpaCreate: false,
		wantHpaCreate: true,
		wantKpaDelete: true,
		wantHpaDelete: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			kubeClient := fakeK8s.NewSimpleClientset()
			servingClient := fakeKna.NewSimpleClientset()

			createdCh := make(chan struct{})
			defer close(createdCh)
			deletedCh := make(chan struct{})
			defer close(deletedCh)

			opts := reconciler.Options{
				KubeClientSet:    kubeClient,
				ServingClientSet: servingClient,
				Logger:           TestLogger(t),
			}

			servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
			kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

			scaleClient := &scalefake.FakeScaleClient{}
			kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

			fakeMetrics := newTestKPAMetrics(createdCh, deletedCh)
			ctl := NewController(&opts,
				servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
				kubeInformer.Core().V1().Endpoints(),
				kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
				fakeMetrics,
				kpaScaler,
			)

			// Create a revision.
			rev := newTestRevision(testNamespace, testRevision, tc.annotations)
			servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
			servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
			ep := addEndpoint(makeEndpoints(rev))
			kubeClient.CoreV1().Endpoints(testNamespace).Create(ep)
			kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
			kpa := revisionresources.MakeKPA(rev)
			servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
			servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)
			reconcileDone := make(chan struct{})
			go func() {
				defer close(reconcileDone)
				err := ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
				if err != nil {
					t.Errorf("Reconcile() = %v", err)
				}
			}()

			// Ensure revision creation has been seen before deleting it.
			if tc.wantKpaCreate {
				select {
				case <-createdCh:
				case <-time.After(3 * time.Second):
					t.Fatal("KPA creation notification timed out")
				}
			}
			if tc.wantKpaDelete {
				select {
				case <-deletedCh:
				case <-time.After(3 * time.Second):
					t.Fatal("KPA deletion notification timed out")
				}
			}

			// Wait for the creation Reconcile to complete.
			_ = <-reconcileDone

			if tc.wantKpaCreate {
				if count := fakeMetrics.createCallCount.Load(); count != 1 {
					t.Fatalf("Create called %d times instead of once", count)
				}
			}
			if tc.wantHpaCreate {
				_, err := kubeClient.AutoscalingV1().HorizontalPodAutoscalers(rev.Namespace).Get(rev.Name, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					t.Errorf("Wanted HPA. Not found")
				} else if err != nil {
					t.Fatalf("Error getting HPA: %v", err)
				}
			}
			if tc.wantKpaDelete {
				if count := fakeMetrics.deleteCallCount.Load(); count != 1 {
					t.Fatalf("Delete called %d times instead of once", count)
				}
			}
			if tc.wantHpaDelete {
				_, err := kubeClient.AutoscalingV1().HorizontalPodAutoscalers(rev.Namespace).Get(rev.Name, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					// Expected
				} else if err != nil {
					t.Fatalf("Error getting HPA: %v", err)
				} else {
					t.Errorf("Did not want HPA. Found HPA.")
				}
			}

			newKPA, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
				kpa.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Get() = %v", err)
			}
			if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "True" {
				t.Errorf("GetCondition(Ready) = %v, wanted True", cond)
			}

			// Delete the revision.
			fakeMetrics.reset()
			servingClient.ServingV1alpha1().Revisions(testNamespace).Delete(testRevision, nil)
			servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Delete(rev)
			servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Delete(testRevision, nil)
			servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Delete(kpa)

			reconcileDone = make(chan struct{})
			go func() {
				defer close(reconcileDone)
				err = ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
				if err != nil {
					t.Errorf("Reconcile() = %v", err)
				}
			}()

			// Expect a KPA deletion.
			select {
			case <-deletedCh:
			case <-time.After(3 * time.Second):
				t.Fatal("KPA deletion notification timed out")
			}

			// Wait for the deletion Reconcile to complete.
			_ = <-reconcileDone

			if count := fakeMetrics.deleteCallCount.Load(); count != 1 {
				t.Fatalf("Delete called %d times instead of once", count)
			}
			_, err = kubeClient.AutoscalingV1().HorizontalPodAutoscalers(rev.Namespace).Get(rev.Name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				// Expected
			} else if err != nil {
				t.Fatalf("Error getting HPA: %v", err)
			} else {
				t.Errorf("Did not want HPA. Found HPA.")
			}
		})
	}
}

func TestNoEndpoints(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	createdCh := make(chan struct{})
	defer close(createdCh)
	deletedCh := make(chan struct{})
	defer close(deletedCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, deletedCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
		fakeMetrics,
		kpaScaler,
	)

	rev := newTestRevision(testNamespace, testRevision, map[string]string{})
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	// These do not exist yet.
	// ep := addEndpoint(makeEndpoints(rev))
	// kubeClient.CoreV1().Endpoints(testNamespace).Create(ep)
	// kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
	kpa := revisionresources.MakeKPA(rev)
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)
	reconcileDone := make(chan struct{})
	go func() {
		defer close(reconcileDone)
		err := ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
		if err != nil {
			t.Errorf("Reconcile() = %v", err)
		}
	}()

	// Wait for the Reconcile to complete.
	_ = <-createdCh
	_ = <-reconcileDone

	newKPA, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "Unknown" {
		t.Errorf("GetCondition(Ready) = %v, wanted Unknown", cond)
	}
}

func TestEmptyEndpoints(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	createdCh := make(chan struct{})
	defer close(createdCh)
	deletedCh := make(chan struct{})
	defer close(deletedCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, deletedCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
		fakeMetrics,
		kpaScaler,
	)

	rev := newTestRevision(testNamespace, testRevision, map[string]string{})
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	// This is empty still.
	ep := makeEndpoints(rev)
	kubeClient.CoreV1().Endpoints(testNamespace).Create(ep)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
	kpa := revisionresources.MakeKPA(rev)
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)
	reconcileDone := make(chan struct{})
	go func() {
		defer close(reconcileDone)
		err := ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
		if err != nil {
			t.Errorf("Reconcile() = %v", err)
		}
	}()

	// Wait for the Reconcile to complete.
	_ = <-createdCh
	_ = <-reconcileDone

	newKPA, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "Unknown" {
		t.Errorf("GetCondition(Ready) = %v, wanted Unknown", cond)
	}
}

func TestControllerCreateError(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	key := testNamespace + "/" + testRevision
	want := errors.NewBadRequest("asdf")

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
		&failingKPAMetrics{
			getErr:    errors.NewNotFound(kpa.Resource("Metrics"), key),
			createErr: want,
		},
		kpaScaler,
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision, map[string]string{}))
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)

	got := ctl.Reconciler.Reconcile(context.TODO(), key)
	if got != want {
		t.Errorf("Reconcile() = %v, wanted %v", got, want)
	}
}

func TestControllerUpdateError(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	key := testNamespace + "/" + testRevision
	want := errors.NewBadRequest("asdf")

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
		&failingKPAMetrics{
			getErr:    errors.NewNotFound(kpa.Resource("Metrics"), key),
			createErr: want,
		},
		kpaScaler,
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision, map[string]string{}))
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)

	got := ctl.Reconciler.Reconcile(context.TODO(), key)
	if got != want {
		t.Errorf("Reconcile() = %v, wanted %v", got, want)
	}
}

func TestControllerGetError(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	key := testNamespace + "/" + testRevision
	want := errors.NewBadRequest("asdf")

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
		&failingKPAMetrics{
			getErr: want,
		},
		kpaScaler,
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision, map[string]string{}))
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)

	got := ctl.Reconciler.Reconcile(context.TODO(), key)
	if got != want {
		t.Errorf("Reconcile() = %v, wanted %v", got, want)
	}
}

func TestScaleFailure(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	createdCh := make(chan struct{})
	defer close(createdCh)
	deletedCh := make(chan struct{})
	defer close(deletedCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, deletedCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
		fakeMetrics,
		kpaScaler,
	)

	// Only put the KPA in the lister, which will prompt failures scaling it.
	rev := newTestRevision(testNamespace, testRevision, map[string]string{})
	kpa := revisionresources.MakeKPA(rev)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)
	go func() {
		err := ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
		if err == nil {
			t.Error("Reconcile() = nil, wanted error")
		}
	}()

	// Ensure revision creation has been seen before deleting it.
	select {
	case <-createdCh:
	case <-time.After(3 * time.Second):
		t.Fatal("Revision creation notification timed out")
	}
}

func TestBadKey(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
		&failingKPAMetrics{},
		kpaScaler,
	)

	err := ctl.Reconciler.Reconcile(context.TODO(), "too/many/parts")
	if err != nil {
		t.Errorf("Reconcile() = %v", err)
	}
}

func newTestKPAMetrics(createdCh chan struct{}, deletedCh chan struct{}) *testKPAMetrics {
	return &testKPAMetrics{atomic.NewUint32(0), atomic.NewUint32(0), createdCh, deletedCh}
}

type testKPAMetrics struct {
	createCallCount *atomic.Uint32
	deleteCallCount *atomic.Uint32
	createdCh       chan struct{}
	deletedCh       chan struct{}
}

func (km *testKPAMetrics) Get(ctx context.Context, key string) (*autoscaler.Metric, error) {
	return nil, errors.NewNotFound(kpa.Resource("Metrics"), key)
}

func (km *testKPAMetrics) Create(ctx context.Context, kpa *kpa.PodAutoscaler) (*autoscaler.Metric, error) {
	km.createCallCount.Add(1)
	km.createdCh <- struct{}{}
	return &autoscaler.Metric{1}, nil
}

func (km *testKPAMetrics) Delete(ctx context.Context, key string) error {
	km.deleteCallCount.Add(1)
	km.deletedCh <- struct{}{}
	return nil
}

func (km *testKPAMetrics) Watch(fn func(string)) {
}

func (km *testKPAMetrics) reset() {
	km.createCallCount.Store(0)
	km.deleteCallCount.Store(0)
}

type failingKPAMetrics struct {
	getErr    error
	createErr error
	deleteErr error
}

func (km *failingKPAMetrics) Get(ctx context.Context, key string) (*autoscaler.Metric, error) {
	return nil, km.getErr
}

func (km *failingKPAMetrics) Create(ctx context.Context, kpa *kpa.PodAutoscaler) (*autoscaler.Metric, error) {
	return nil, km.createErr
}

func (km *failingKPAMetrics) Delete(ctx context.Context, key string) error {
	return km.deleteErr
}

func (km *failingKPAMetrics) Watch(fn func(string)) {
}

func newTestRevision(namespace string, name string, annotations map[string]string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:    fmt.Sprintf("/apis/ela/v1alpha1/namespaces/%s/revisions/%s", namespace, name),
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1alpha1.RevisionSpec{
			ServingState: "Active",
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

func makeEndpoints(rev *v1alpha1.Revision) *corev1.Endpoints {
	service := revisionresources.MakeK8sService(rev)
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
}

func addEndpoint(ep *corev1.Endpoints) *corev1.Endpoints {
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{IP: "127.0.0.1"}},
	}}
	return ep
}
