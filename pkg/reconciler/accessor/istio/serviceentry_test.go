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
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/controller"
	istioclientset "knative.dev/serving/pkg/client/istio/clientset/versioned"
	istiofake "knative.dev/serving/pkg/client/istio/clientset/versioned/fake"
	istioinformers "knative.dev/serving/pkg/client/istio/informers/externalversions"
	fakeistioclient "knative.dev/serving/pkg/client/istio/injection/client/fake"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"

	. "knative.dev/pkg/reconciler/testing"
)

var (
	orgSe = &v1alpha3.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "se",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: istiov1alpha3.ServiceEntry{
			Hosts: []string{"origin.example.com"},
		},
	}

	desiredSe = &v1alpha3.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "se",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: istiov1alpha3.ServiceEntry{
			Hosts: []string{"desired.example.com"},
		},
	}
)

type FakeServiceEntryAccessor struct {
	client   istioclientset.Interface
	seLister istiolisters.ServiceEntryLister
}

func (f *FakeServiceEntryAccessor) GetIstioClient() istioclientset.Interface {
	return f.client
}

func (f *FakeServiceEntryAccessor) GetServiceEntryLister() istiolisters.ServiceEntryLister {
	return f.seLister
}

func TestReconcileServiceEntry_Create(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctx, cancel := context.WithCancel(ctx)

	istioClient := fakeistioclient.Get(ctx)

	h := NewHooks()
	h.OnCreate(&istioClient.Fake, "serviceentries", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha3.ServiceEntry)
		if diff := cmp.Diff(got, desiredSe); diff != "" {
			t.Logf("Unexpected ServiceEntry (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	accessor, waitInformers := setupServiceEntry(ctx, []*v1alpha3.ServiceEntry{}, istioClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	ReconcileServiceEntry(ctx, ownerObj, desiredSe, accessor)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile ServiceEntry: %v", err)
	}
}

func TestReconcileServiceEntry_Update(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctx, cancel := context.WithCancel(ctx)

	istioClient := fakeistioclient.Get(ctx)
	accessor, waitInformers := setupServiceEntry(ctx, []*v1alpha3.ServiceEntry{orgSe}, istioClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	h := NewHooks()
	h.OnUpdate(&istioClient.Fake, "serviceentries", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha3.ServiceEntry)
		if diff := cmp.Diff(got, desiredSe); diff != "" {
			t.Logf("Unexpected ServiceEntry (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileServiceEntry(ctx, ownerObj, desiredSe, accessor)
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile ServiceEntry: %v", err)
	}
}

func setupServiceEntry(ctx context.Context, ses []*v1alpha3.ServiceEntry,
	istioClient istioclientset.Interface, t *testing.T) (*FakeServiceEntryAccessor, func()) {

	fake := istiofake.NewSimpleClientset()
	informer := istioinformers.NewSharedInformerFactory(fake, 0)
	seInformer := informer.Networking().V1alpha3().ServiceEntries()

	for _, se := range ses {
		fake.NetworkingV1alpha3().ServiceEntries(se.Namespace).Create(se)
		seInformer.Informer().GetIndexer().Add(se)
	}

	waitInformers, err := controller.RunInformers(ctx.Done(), seInformer.Informer())
	if err != nil {
		t.Fatalf("failed to start virtualservice informer: %v", err)
	}

	return &FakeServiceEntryAccessor{
		client:   istioClient,
		seLister: seInformer.Lister(),
	}, waitInformers
}
