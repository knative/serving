/*
Copyright 2020 The Knative Authors

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

package statforwarder

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	ktesting "k8s.io/client-go/testing"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeleaseinformer "knative.dev/pkg/client/injection/kube/informers/coordination/v1/lease/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/logging"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	bucket1 = "as-bucket-00-of-02"
	bucket2 = "as-bucket-01-of-02"
	testIP1 = "1.23.456.789"
	testIP2 = "0.23.456.789"
)

var (
	testHolder1 = "autoscaler-1_" + testIP1
	testHolder2 = "autoscaler-2_" + testIP2
	testNs      = system.Namespace()
	testBs      = hash.NewBucketSet(sets.NewString(bucket1))
	testLease   = &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucket1,
			Namespace: testNs,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: &testHolder1,
		},
	}
	stat1 = asmetrics.StatMessage{
		Key: types.NamespacedName{
			Namespace: testNs,
			Name:      "succulent", // Mapped to bucket1
		},
	}
	stat2 = asmetrics.StatMessage{
		Key: types.NamespacedName{
			Namespace: testNs,
			Name:      "plant", // Mapped to bucket2
		},
	}
	// A statProcessor doing nothing.
	noOp = func(sm asmetrics.StatMessage) {}
)

func TestForwarderReconcile(t *testing.T) {
	ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
	logger := logging.FromContext(ctx)
	kubeClient := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	service := fakeserviceinformer.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	f1 := New(ctx, logger, kubeClient, testIP1, testBs, noOp)
	f2 := New(ctx, logger, kubeClient, testIP2, testBs, noOp)

	defer func() {
		f1.Cancel()
		f2.Cancel()
		cancel()
		waitInformers()
	}()

	kubeClient.CoordinationV1().Leases(testNs).Create(ctx, testLease, metav1.CreateOptions{})
	lease.Informer().GetIndexer().Add(testLease)

	var lastErr error
	// Wait for the resources to be created.
	if err := wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
		_, lastErr = service.Lister().Services(testNs).Get(bucket1)
		return lastErr == nil, nil
	}); err != nil {
		t.Fatal("Timeout to get the Service:", lastErr)
	}

	wantSubsets := []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{
			IP: testIP1,
		}},
		Ports: []corev1.EndpointPort{{
			Name:     autoscalerPortName,
			Port:     autoscalerPort,
			Protocol: corev1.ProtocolTCP,
		}}},
	}

	// Check the endpoints got updated.
	el := endpoints.Lister().Endpoints(testNs)
	if err := wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
		got, err := el.Get(bucket1)
		if err != nil {
			lastErr = err
			return false, nil
		}

		if !cmp.Equal(wantSubsets, got.Subsets) {
			lastErr = fmt.Errorf("Got Subsets = %v, want = %v", got.Subsets, wantSubsets)
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatal("Timeout to get the Endpoints:", lastErr)
	}

	// Lease holder gets changed.
	l := testLease.DeepCopy()
	l.Spec.HolderIdentity = &testHolder2
	kubeClient.CoordinationV1().Leases(testNs).Update(ctx, l, metav1.UpdateOptions{})
	lease.Informer().GetIndexer().Add(l)

	// Check that the endpoints got updated.
	wantSubsets[0].Addresses[0].IP = testIP2
	if err := wait.PollImmediate(10*time.Millisecond, 10*time.Second, func() (bool, error) {
		// Check the endpoints get updated.
		got, err := el.Get(bucket1)
		if err != nil {
			lastErr = err
			return false, nil
		}

		if !cmp.Equal(wantSubsets, got.Subsets) {
			lastErr = fmt.Errorf("Got Subsets = %v, want = %v", got.Subsets, wantSubsets)
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatal("Timeout to get the Endpoints:", lastErr)
	}
}

func TestForwarderRetryOnSvcCreationFailure(t *testing.T) {
	ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
	logger := logging.FromContext(ctx)
	kubeClient := fakekubeclient.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	defer func() {
		cancel()
		waitInformers()
	}()

	New(ctx, logger, kubeClient, testIP1, testBs, noOp)

	svcCreation := 0
	retried := make(chan struct{})
	kubeClient.PrependReactor("create", "services",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			svcCreation++
			if svcCreation == 2 {
				retried <- struct{}{}
				return true, nil, nil
			}
			return true, nil, errors.New("Failed to create")
		},
	)

	kubeClient.CoordinationV1().Leases(testNs).Create(ctx, testLease, metav1.CreateOptions{})
	lease.Informer().GetIndexer().Add(testLease)

	select {
	case <-retried:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for SVC retry")
	}
}

func TestForwarderRetryOnEndpointsCreationFailure(t *testing.T) {
	ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
	logger := logging.FromContext(ctx)
	kubeClient := fakekubeclient.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	defer func() {
		cancel()
		waitInformers()
	}()

	New(ctx, logger, kubeClient, testIP1, testBs, noOp)

	endpointsCreation := 0
	retried := make(chan struct{})
	kubeClient.PrependReactor("create", "endpoints",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			endpointsCreation++
			if endpointsCreation == 2 {
				retried <- struct{}{}
				return true, nil, nil
			}
			return true, nil, errors.New("Failed to create")
		},
	)

	kubeClient.CoordinationV1().Leases(testNs).Create(ctx, testLease, metav1.CreateOptions{})
	lease.Informer().GetIndexer().Add(testLease)

	select {
	case <-retried:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for Endpoints retry")
	}
}

func TestForwarderRetryOnEndpointsUpdateFailure(t *testing.T) {
	ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
	logger := logging.FromContext(ctx)
	kubeClient := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	defer func() {
		cancel()
		waitInformers()
	}()

	New(ctx, logger, kubeClient, testIP1, testBs, noOp)

	endpointsUpdate := 0
	retried := make(chan struct{})
	kubeClient.PrependReactor("update", "endpoints",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			endpointsUpdate++
			if endpointsUpdate == 2 {
				retried <- struct{}{}
				return true, nil, nil
			}
			return true, nil, errors.New("Failed to update")
		},
	)

	e := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucket1,
			Namespace: testNs,
		},
	}
	kubeClient.CoreV1().Endpoints(testNs).Create(ctx, e, metav1.CreateOptions{})
	endpoints.Informer().GetIndexer().Add(e)
	kubeClient.CoordinationV1().Leases(testNs).Create(ctx, testLease, metav1.CreateOptions{})
	lease.Informer().GetIndexer().Add(testLease)

	select {
	case <-retried:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for Endpoints retry")
	}
}

func TestForwarderSkipReconciling(t *testing.T) {
	ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
	logger := logging.FromContext(ctx)
	kubeClient := fakekubeclient.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	defer func() {
		cancel()
		waitInformers()
	}()

	New(ctx, logger, kubeClient, testIP1, testBs, noOp)

	svcCreated := make(chan struct{})
	kubeClient.PrependReactor("create", "services",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			svcCreated <- struct{}{}
			return false, nil, nil
		},
	)
	endpointsCreated := make(chan struct{})
	kubeClient.PrependReactor("create", "endpoints",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			endpointsCreated <- struct{}{}
			return false, nil, nil
		},
	)

	testCases := []struct {
		description string
		namespace   string
		name        string
		holder      string
	}{{
		description: "not autoscaler bucket lease",
		namespace:   testNs,
		name:        bucket2,
		holder:      testHolder1,
	}, {
		description: "different namespace",
		namespace:   "other-ns",
		name:        bucket1,
		holder:      testHolder1,
	}, {
		description: "without holder",
		namespace:   testNs,
		name:        bucket1,
	}, {
		description: "not the holder",
		namespace:   testNs,
		name:        bucket1,
		holder:      testHolder2,
	}}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			l := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.name,
					Namespace: tc.namespace,
				},
			}
			if tc.holder != "" {
				l.Spec = coordinationv1.LeaseSpec{
					HolderIdentity: &tc.holder,
				}
			}
			kubeClient.CoordinationV1().Leases(testNs).Create(ctx, l, metav1.CreateOptions{})
			lease.Informer().GetIndexer().Add(l)

			select {
			case <-svcCreated:
				t.Error("Got Service created, want no actions")
			case <-time.After(50 * time.Millisecond):
			}
			select {
			case <-endpointsCreated:
				t.Error("Got Endpoints created, want no actions")
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

func TestProcess(t *testing.T) {
	ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
	logger := logging.FromContext(ctx)
	kubeClient := fakekubeclient.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	defer func() {
		cancel()
		waitInformers()
	}()

	// Make a buffered channel so it won't block the forwarder process.
	acceptCh := make(chan int, 2)
	acceptCount := 0
	accept := func(sm asmetrics.StatMessage) {
		acceptCount++
		acceptCh <- acceptCount
	}
	f := New(ctx, logger, kubeClient, testIP1, hash.NewBucketSet(sets.NewString(bucket1, bucket2)), accept)

	// A Forward without any leadership information should process with retry.
	// Stat1 should be accepted and stat2 should be forwarded.
	f.Process(stat1)
	f.Process(stat2)

	kubeClient.CoordinationV1().Leases(testNs).Create(ctx, testLease, metav1.CreateOptions{})
	lease.Informer().GetIndexer().Add(testLease)

	anotherLease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucket2,
			Namespace: testNs,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: &testHolder2,
		},
	}
	kubeClient.CoordinationV1().Leases(testNs).Create(ctx, anotherLease, metav1.CreateOptions{})
	lease.Informer().GetIndexer().Add(anotherLease)

	// Wait for the forwarder to become the leader for bucket1.
	if err := wait.PollImmediate(10*time.Millisecond, 10*time.Second, func() (bool, error) {
		p1 := f.getProcessor(bucket1)
		p2 := f.getProcessor(bucket2)
		return p1 != nil && p2 != nil && p1.ip == testIP1 && p2.ip == testIP2, nil
	}); err != nil {
		t.Fatalf("Timeout waiting f.processors got updated")
	}

	// Wait for the stat enqueued previously to be retried.
	got := <-acceptCh
	if got != 1 {
		t.Fatalf("Got = %v, want: 1", got)
	}

	// Accept once more.
	f.Process(stat1)

	got = <-acceptCh
	if got != 2 {
		t.Fatalf("Got = %v, want: 2", got)
	}

	// Make sure Cancel can be called without crash.
	f.Cancel()
}

func TestIsBucketOwner(t *testing.T) {
	f := Forwarder{
		processors: map[string]*bucketProcessor{
			bucket1: {
				bkt:    bucket1,
				accept: noOp,
			},
			bucket2: {
				bkt: bucket2,
			},
		},
	}

	if got := f.IsBucketOwner(bucket1); got != true {
		t.Errorf("IsBktOwner(bucket1) = %v, want false", got)
	}
	if got := f.IsBucketOwner(bucket2); got != false {
		t.Errorf("IsBktOwner(bucket2) = %v, want true", got)
	}
	if got := f.IsBucketOwner("not-in-record"); got != false {
		t.Errorf("IsBktOwner(not-in-record) = %v, want true", got)
	}
}
