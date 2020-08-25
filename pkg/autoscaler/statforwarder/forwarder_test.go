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
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
	coordinationv1 "k8s.io/api/coordination/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	ktesting "k8s.io/client-go/testing"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeleaseinformer "knative.dev/pkg/client/injection/kube/informers/coordination/v1/lease/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
)

const (
	testIP1 = "1.23.456.789"
	testIP2 = "0.23.456.789"
	bucket1 = "as-bucket-00-of-02"
	bucket2 = "as-bucket-01-of-02"
)

var testBs = hash.NewBucketSet(sets.NewString(bucket1))

func TestForwarderReconcile(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	kubeClient := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	service := fakeserviceinformer.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(
		ctx.Done(), endpoints.Informer(), service.Informer(), lease.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	t.Cleanup(func() {
		cancel()
		waitInformers()
	})

	New(ctx, zap.NewNop().Sugar(), kubeClient, testIP1, testBs)

	created := make(chan struct{})
	kubeClient.PrependReactor("create", "services",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			created <- struct{}{}
			return false, nil, nil
		},
	)

	ns := system.Namespace()
	holder := testIP1
	l := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucket1,
			Namespace: ns,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: &holder,
		},
	}
	kubeClient.CoordinationV1().Leases(ns).Create(l)
	lease.Informer().GetIndexer().Add(l)

	// Wait Serice to be created.
	select {
	case <-created:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for Service creation.")
	}

	var lastErr error
	// Wait for the resources to be created.
	if err := wait.PollImmediate(100*time.Millisecond, 2*time.Second, func() (bool, error) {
		_, err := service.Lister().Services(ns).Get(bucket1)
		lastErr = err
		return err == nil, nil
	}); err != nil {
		t.Fatalf("Timeout to get the Service: %v", lastErr)
	}

	wantSubsets := []v1.EndpointSubset{{
		Addresses: []v1.EndpointAddress{{
			IP: testIP1,
		}},
		Ports: []v1.EndpointPort{{
			Name:     autoscalerPortName,
			Port:     autoscalerPort,
			Protocol: v1.ProtocolTCP,
		}}},
	}

	if err := wait.PollImmediate(100*time.Millisecond, 2*time.Second, func() (bool, error) {
		// Check the endpoints get updated.
		got, err := endpoints.Lister().Endpoints(ns).Get(bucket1)
		if err != nil {
			lastErr = err
			return false, nil
		}

		if equality.Semantic.DeepEqual(wantSubsets, got.Subsets) {
			return true, nil
		}

		lastErr = fmt.Errorf("Got Subsets = %v, want = %v", got.Subsets, wantSubsets)
		return false, nil
	}); err != nil {
		t.Fatalf("Timeout to get the Endpoints: %v", lastErr)
	}

	holder = testIP2
	l.Spec.HolderIdentity = &holder
	kubeClient.CoordinationV1().Leases(ns).Update(l)

	// Should not create a Service as there's already one.
	select {
	case <-created:
		t.Error("Got Service created, want no actions.")
	case <-time.After(time.Second):
	}

	if err := wait.PollImmediate(100*time.Millisecond, 2*time.Second, func() (bool, error) {
		// Check the endpoints get updated.
		got, err := endpoints.Lister().Endpoints(ns).Get(bucket1)
		if err != nil {
			lastErr = err
			return false, nil
		}

		if equality.Semantic.DeepEqual(wantSubsets, got.Subsets) {
			return true, nil
		}

		lastErr = fmt.Errorf("Got Subsets = %v, want = %v", got.Subsets, wantSubsets)
		return false, nil
	}); err != nil {
		t.Fatalf("Timeout to get the Endpoints: %v", lastErr)
	}
}

func TestForwarderSkipReconciling(t *testing.T) {
	ns := system.Namespace()
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	kubeClient := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	service := fakeserviceinformer.Get(ctx)
	lease := fakeleaseinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(
		ctx.Done(), endpoints.Informer(), service.Informer(), lease.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	t.Cleanup(func() {
		cancel()
		waitInformers()
	})

	New(ctx, zap.NewNop().Sugar(), kubeClient, testIP1, testBs)

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
		namespace:   ns,
		name:        bucket2,
		holder:      testIP1,
	}, {
		description: "different namespace",
		namespace:   "other-ns",
		name:        bucket1,
		holder:      testIP1,
	}, {
		description: "without holder",
		namespace:   ns,
		name:        bucket1,
	}, {
		description: "not the holder",
		namespace:   ns,
		name:        bucket1,
		holder:      testIP2,
	}}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			l := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.name,
					Namespace: tc.namespace,
				},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: &tc.holder,
				},
			}
			kubeClient.CoordinationV1().Leases(ns).Create(l)
			lease.Informer().GetIndexer().Add(l)

			select {
			case <-svcCreated:
				t.Error("Got Service created, want no actions")
			case <-time.After(time.Second):
			}
			select {
			case <-endpointsCreated:
				t.Error("Got Endpoints created, want no actions")
			case <-time.After(time.Second):
			}
		})
	}
}
