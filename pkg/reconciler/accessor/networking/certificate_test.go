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

package networking

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/controller"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	sharedfake "knative.dev/serving/pkg/client/clientset/versioned/fake"
	informers "knative.dev/serving/pkg/client/informers/externalversions"
	fakeclient "knative.dev/serving/pkg/client/injection/client/fake"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"

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

	origin = &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cert",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{"origin.example.com"},
			SecretName: "secret0",
		},
	}

	desired = &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cert",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{"desired.example.com"},
			SecretName: "secret0",
		},
	}
)

type FakeAccessor struct {
	client     clientset.Interface
	certLister listers.CertificateLister
}

func (f *FakeAccessor) GetServingClient() clientset.Interface {
	return f.client
}

func (f *FakeAccessor) GetCertificateLister() listers.CertificateLister {
	return f.certLister
}

func TestReconcileCertificateCreate(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	grp := errgroup.Group{}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
	}()

	client := fakeclient.Get(ctx)

	h := NewHooks()
	h.OnCreate(&client.Fake, "certificates", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha1.Certificate)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Logf("Unexpected Certificate (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	accessor := setup(ctx, []*v1alpha1.Certificate{}, client, t)
	ReconcileCertificate(ctx, ownerObj, desired, accessor)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile Certificate: %v", err)
	}
}

func TestReconcileCertificateUpdate(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	grp := errgroup.Group{}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
	}()

	client := fakeclient.Get(ctx)
	accessor := setup(ctx, []*v1alpha1.Certificate{origin}, client, t)

	h := NewHooks()
	h.OnUpdate(&client.Fake, "certificates", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha1.Certificate)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Logf("Unexpected Certificate (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileCertificate(ctx, ownerObj, desired, accessor)
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile Certificate: %v", err)
	}
}

func setup(ctx context.Context, certs []*v1alpha1.Certificate,
	client clientset.Interface, t *testing.T) *FakeAccessor {

	fake := sharedfake.NewSimpleClientset()
	informer := informers.NewSharedInformerFactory(fake, 0)
	certInformer := informer.Networking().V1alpha1().Certificates()

	for _, cert := range certs {
		fake.NetworkingV1alpha1().Certificates(cert.Namespace).Create(cert)
		certInformer.Informer().GetIndexer().Add(cert)
	}

	if err := controller.StartInformers(ctx.Done(), certInformer.Informer()); err != nil {
		t.Fatalf("failed to start Certificate informer: %v", err)
	}

	return &FakeAccessor{
		client:     client,
		certLister: certInformer.Lister(),
	}
}
