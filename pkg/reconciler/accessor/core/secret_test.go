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
package core

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"

	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakesecretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"

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

	origin = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "secret",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Data: map[string][]byte{
			"test-secret": []byte("origin"),
		},
	}

	desired = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "secret",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Data: map[string][]byte{
			"test-secret": []byte("desired"),
		},
	}

	notOwnedSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"test-secret": []byte("origin"),
		},
	}
)

type FakeAccessor struct {
	client       kubernetes.Interface
	secretLister corev1listers.SecretLister
}

func (f *FakeAccessor) GetKubeClient() kubernetes.Interface {
	return f.client
}

func (f *FakeAccessor) GetSecretLister() corev1listers.SecretLister {
	return f.secretLister
}

func TestReconcileSecretCreate(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	kubeClient := fakekubeclient.Get(ctx)

	h := NewHooks()
	h.OnCreate(&kubeClient.Fake, "secrets", func(obj runtime.Object) HookResult {
		got := obj.(*corev1.Secret)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Logf("Unexpected Secret (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	accessor, waitInformers := setup(ctx, []*corev1.Secret{}, kubeClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	ReconcileSecret(ctx, ownerObj, desired, accessor)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile Secret: %v", err)
	}
}

func TestReconcileSecretUpdate(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)

	kubeClient := fakekubeclient.Get(ctx)
	accessor, waitInformers := setup(ctx, []*corev1.Secret{origin}, kubeClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	h := NewHooks()
	h.OnUpdate(&kubeClient.Fake, "secrets", func(obj runtime.Object) HookResult {
		got := obj.(*corev1.Secret)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Logf("Unexpected Secret (-want, +got): %v", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileSecret(ctx, ownerObj, desired, accessor)
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Errorf("Failed to Reconcile Secret: %v", err)
	}
}

func TestNotOwnedFailure(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)

	kubeClient := fakekubeclient.Get(ctx)
	accessor, waitInformers := setup(ctx, []*corev1.Secret{notOwnedSecret}, kubeClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	_, err := ReconcileSecret(ctx, ownerObj, desired, accessor)
	if err == nil {
		t.Error("Expected to get error when calling ReconcileSecret, but got no error.")
	}
	if !kaccessor.IsNotOwned(err) {
		t.Errorf("Expected to get NotOwnedError but got %v", err)
	}
}

func setup(ctx context.Context, secrets []*corev1.Secret,
	kubeClient kubernetes.Interface, t *testing.T) (*FakeAccessor, func()) {

	secretInformer := fakesecretinformer.Get(ctx)

	fake := fakekubeclient.Get(ctx)
	for _, secret := range secrets {
		fake.CoreV1().Secrets(secret.Namespace).Create(secret)
		secretInformer.Informer().GetIndexer().Add(secret)
	}

	waitInformers, err := controller.RunInformers(ctx.Done(), secretInformer.Informer())
	if err != nil {
		t.Fatalf("failed to start secret informer: %v", err)
	}

	return &FakeAccessor{
		client:       kubeClient,
		secretLister: secretInformer.Lister(),
	}, waitInformers
}
