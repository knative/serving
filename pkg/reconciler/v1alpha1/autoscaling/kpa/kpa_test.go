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

package kpa

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/knative/pkg/configmap"
	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/apis/autoscaling"
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
	gracePeriod   = 60 * time.Second
	stableWindow  = 5 * time.Minute
	configMapData = map[string]string{
		"max-scale-up-rate":                       "1.0",
		"container-concurrency-target-percentage": "0.5",
		"container-concurrency-target-default":    "10.0",
		"stable-window":                           stableWindow.String(),
		"panic-window":                            "10s",
		"scale-to-zero-grace-period":              gracePeriod.String(),
		"tick-interval":                           "2s",
	}
)

func newConfigWatcher() configmap.Watcher {
	return configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      autoscaler.ConfigName,
		},
		Data: configMapData,
	})
}

func newDynamicConfig(t *testing.T) *autoscaler.DynamicConfig {
	dynConfig, err := autoscaler.NewDynamicConfigFromMap(configMapData, TestLogger(t))
	if err != nil {
		t.Errorf("Error creating DynamicConfig: %v", err)
	}
	return dynConfig
}

// TODO(josephburnett): Convert KPA tests to table tests.

func TestControllerSynchronizesCreatesAndDeletes(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	stopCh := make(chan struct{})
	createdCh := make(chan struct{})
	defer close(createdCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, stopCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		fakeMetrics,
		kpaScaler,
		newDynamicConfig(t),
	)

	rev := newTestRevision(testNamespace, testRevision)
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
	select {
	case <-createdCh:
	case <-time.After(3 * time.Second):
		t.Fatal("Revision creation notification timed out")
	}

	// Wait for the Reconcile to complete.
	_ = <-reconcileDone

	if count := fakeMetrics.createCallCount.Load(); count != 1 {
		t.Fatalf("Create called %d times instead of once", count)
	}

	newKPA, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "True" {
		t.Errorf("GetCondition(Ready) = %v, wanted True", cond)
	}

	servingClient.ServingV1alpha1().Revisions(testNamespace).Delete(testRevision, nil)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Delete(rev)
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Delete(testRevision, nil)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Delete(kpa)
	err = ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
	if err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	if fakeMetrics.deleteCallCount.Load() == 0 {
		t.Fatal("Delete was not called")
	}

	if fakeMetrics.deleteBeforeCreate.Load() {
		t.Fatal("Delete ran before OnPresent")
	}
}

func TestUpdate(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	stopCh := make(chan struct{})
	createdCh := make(chan struct{})
	defer close(createdCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, stopCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		fakeMetrics,
		kpaScaler,
		newDynamicConfig(t),
	)

	rev := newTestRevision(testNamespace, testRevision)
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

	// Ensure revision creation has been seen before updating it.
	select {
	case <-createdCh:
	case <-time.After(3 * time.Second):
		t.Fatal("Revision creation notification timed out")
	}

	// Wait for the Reconcile to complete.
	_ = <-reconcileDone

	if count := fakeMetrics.createCallCount.Load(); count != 1 {
		t.Fatalf("Create called %d times instead of once", count)
	}

	newKPA, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "True" {
		t.Errorf("GetCondition(Ready) = %v, wanted True", cond)
	}

	// Update the KPA container concurrency.
	kpa.Spec.ContainerConcurrency = 2
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Update(kpa)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Update(kpa)

	reconcileDone = make(chan struct{})
	go func() {
		defer close(reconcileDone)
		err = ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
		if err != nil {
			t.Errorf("Reconcile() = %v", err)
		}
	}()
	_ = <-reconcileDone

	if fakeMetrics.updateCallCount.Load() == 0 {
		t.Fatal("Update was not called")
	}
}

func TestNonKpaClass(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	stopCh := make(chan struct{})
	createdCh := make(chan struct{})
	defer close(createdCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, stopCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		fakeMetrics,
		kpaScaler,
		newDynamicConfig(t),
	)

	rev := newTestRevision(testNamespace, testRevision)
	rev.Annotations = map[string]string{
		autoscaling.ClassAnnotationKey: autoscaling.HPA, // non "kpa" class
	}

	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
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

	// Wait for reconcile to finish
	select {
	case <-reconcileDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Reconciliation timed out")
	}

	// Verify no KPAMetrics were created
	if fakeMetrics.createCallCount.Load() != 0 {
		t.Errorf("Unexpected KPAMetrics created")
	}
}

func TestNoEndpoints(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	stopCh := make(chan struct{})
	createdCh := make(chan struct{})
	defer close(createdCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, stopCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		fakeMetrics,
		kpaScaler,
		newDynamicConfig(t),
	)

	rev := newTestRevision(testNamespace, testRevision)
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

	stopCh := make(chan struct{})
	createdCh := make(chan struct{})
	defer close(createdCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, stopCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		fakeMetrics,
		kpaScaler,
		newDynamicConfig(t),
	)

	rev := newTestRevision(testNamespace, testRevision)
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
		&failingKPAMetrics{
			getErr:    errors.NewNotFound(kpa.Resource("Metrics"), key),
			createErr: want,
		},
		kpaScaler,
		newDynamicConfig(t),
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision))
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
		&failingKPAMetrics{
			getErr:    errors.NewNotFound(kpa.Resource("Metrics"), key),
			createErr: want,
		},
		kpaScaler,
		newDynamicConfig(t),
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision))
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
		&failingKPAMetrics{
			getErr: want,
		},
		kpaScaler,
		newDynamicConfig(t),
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision))
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

	stopCh := make(chan struct{})
	createdCh := make(chan struct{})
	defer close(createdCh)

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	scaleClient := &scalefake.FakeScaleClient{}
	kpaScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

	fakeMetrics := newTestKPAMetrics(createdCh, stopCh)
	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Core().V1().Endpoints(),
		fakeMetrics,
		kpaScaler,
		newDynamicConfig(t),
	)

	// Only put the KPA in the lister, which will prompt failures scaling it.
	rev := newTestRevision(testNamespace, testRevision)
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
		&failingKPAMetrics{},
		kpaScaler,
		newDynamicConfig(t),
	)

	err := ctl.Reconciler.Reconcile(context.TODO(), "too/many/parts")
	if err != nil {
		t.Errorf("Reconcile() = %v", err)
	}
}

func newTestKPAMetrics(createdCh chan struct{}, stopCh chan struct{}) *testKPAMetrics {
	return &testKPAMetrics{
		createCallCount:    atomic.NewUint32(0),
		deleteCallCount:    atomic.NewUint32(0),
		updateCallCount:    atomic.NewUint32(0),
		deleteBeforeCreate: atomic.NewBool(false),
		createdCh:          createdCh,
		stopCh:             stopCh,
	}
}

type testKPAMetrics struct {
	createCallCount    *atomic.Uint32
	deleteCallCount    *atomic.Uint32
	updateCallCount    *atomic.Uint32
	deleteBeforeCreate *atomic.Bool
	createdCh          chan struct{}
	stopCh             chan struct{}
	metric             *autoscaler.Metric
}

func (km *testKPAMetrics) Get(ctx context.Context, namespace, name string) (*autoscaler.Metric, error) {
	if km.metric == nil {
		return nil, errors.NewNotFound(kpa.Resource("Metrics"), autoscaler.NewMetricKey(namespace, name))
	}
	return km.metric, nil
}

func (km *testKPAMetrics) Create(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error) {
	km.metric = metric
	km.createCallCount.Add(1)
	km.createdCh <- struct{}{}
	return metric, nil
}

func (km *testKPAMetrics) Delete(ctx context.Context, namespace, name string) error {
	km.metric = nil
	km.deleteCallCount.Add(1)
	if km.createCallCount.Load() > 0 {
		// OnAbsent may be called more than once
		if km.deleteCallCount.Load() == 1 {
			close(km.stopCh)
		}
	} else {
		km.deleteBeforeCreate.Store(true)
	}
	return nil
}

func (km *testKPAMetrics) Update(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error) {
	km.metric = metric
	km.updateCallCount.Add(1)
	return metric, nil
}

func (km *testKPAMetrics) Watch(fn func(string)) {
}

type failingKPAMetrics struct {
	getErr    error
	createErr error
	deleteErr error
}

func (km *failingKPAMetrics) Get(ctx context.Context, namespace, name string) (*autoscaler.Metric, error) {
	return nil, km.getErr
}

func (km *failingKPAMetrics) Create(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error) {
	return nil, km.createErr
}

func (km *failingKPAMetrics) Delete(ctx context.Context, namespace, name string) error {
	return km.deleteErr
}

func (km *failingKPAMetrics) Watch(fn func(string)) {
}

func (km *failingKPAMetrics) Update(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error) {
	return metric, nil
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
