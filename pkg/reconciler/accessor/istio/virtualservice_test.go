/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

https://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package istio

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis/istio/v1alpha3"
	sharedclientset "knative.dev/pkg/client/clientset/versioned"
	sharedfake "knative.dev/pkg/client/clientset/versioned/fake"
	informers "knative.dev/pkg/client/informers/externalversions"
	fakesharedclient "knative.dev/pkg/client/injection/client/fake"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"

	. "knative.dev/pkg/reconciler/testing"
)

var (
	ownerObj = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ownerObj",
			Namespace: "default",
			UID:       "abcd",
		},
	}

	ownerRef = metav1.OwnerReference{
		Kind:       ownerObj.Kind,
		Name:       ownerObj.Name,
		UID:        ownerObj.UID,
		Controller: ptr.Bool(true),
	}

	origin = &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "vs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: v1alpha3.VirtualServiceSpec{
			Hosts: []string{"origin.example.com"},
		},
	}

	desired = &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "vs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: v1alpha3.VirtualServiceSpec{
			Hosts: []string{"desired.example.com"},
		},
	}
)

type FakeAccessor struct {
	client   sharedclientset.Interface
	vsLister istiolisters.VirtualServiceLister
}

func (f *FakeAccessor) GetSharedClient() sharedclientset.Interface {
	return f.client
}

func (f *FakeAccessor) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return f.vsLister
}

func TestReconcileVirtualService_Create(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctx, cancel := context.WithCancel(ctx)
	waitInformers := func() {}
	defer func() {
		cancel()
		waitInformers()
	}()

	sharedClient := fakesharedclient.Get(ctx)

	h := NewHooks()
	h.OnCreate(&sharedClient.Fake, "virtualservices", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha3.VirtualService)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Logf("Unexpected VirtualService (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	accessor, waitInformers := setup(ctx, []*v1alpha3.VirtualService{}, sharedClient, t)
	ReconcileVirtualService(ctx, ownerObj, desired, accessor)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile VirtualService: %v", err)
	}
}

func TestReconcileVirtualService_Update(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctx, cancel := context.WithCancel(ctx)
	waitInformers := func() {}
	defer func() {
		cancel()
		waitInformers()
	}()

	sharedClient := fakesharedclient.Get(ctx)
	accessor, waitInformers := setup(ctx, []*v1alpha3.VirtualService{origin}, sharedClient, t)

	h := NewHooks()
	h.OnUpdate(&sharedClient.Fake, "virtualservices", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha3.VirtualService)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Logf("Unexpected VirtualService (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileVirtualService(ctx, ownerObj, desired, accessor)
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile VirtualService: %v", err)
	}
}

func setup(ctx context.Context, vses []*v1alpha3.VirtualService,
	sharedClient sharedclientset.Interface, t *testing.T) (*FakeAccessor, func()) {

	fake := sharedfake.NewSimpleClientset()
	informer := informers.NewSharedInformerFactory(fake, 0)
	vsInformer := informer.Networking().V1alpha3().VirtualServices()

	for _, vs := range vses {
		fake.NetworkingV1alpha3().VirtualServices(vs.Namespace).Create(vs)
		vsInformer.Informer().GetIndexer().Add(vs)
	}

	waitInformers, err := controller.RunInformers(ctx.Done(), vsInformer.Informer())
	if err != nil {
		t.Fatalf("failed to start virtualservice informer: %v", err)
	}

	return &FakeAccessor{
		client:   sharedClient,
		vsLister: vsInformer.Lister(),
	}, waitInformers
}
