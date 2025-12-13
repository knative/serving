/*
Copyright 2019 The Knative Authors

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

package certificates

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	certresources "knative.dev/pkg/webhook/certificates/resources"
)

// fakeBucket is a test helper that implements Bucket
type fakeBucket struct {
	key types.NamespacedName
}

func (f *fakeBucket) Name() string {
	return "test-bucket"
}

func (f *fakeBucket) Has(key types.NamespacedName) bool {
	return f.key == key
}

func TestReconcileCertificateWithEmptySecret(t *testing.T) {
	ctx := context.Background()
	namespace := system.Namespace()
	secretName := "webhook-certs"
	serviceName := "webhook"

	// Create a secret with nil Data (simulating reinstall scenario)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: nil,
	}

	client := fake.NewSimpleClientset(secret)
	secretInformer := cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "secrets", namespace, nil),
		&corev1.Secret{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	// Add the secret to the informer
	secretInformer.GetStore().Add(secret)

	secretLister := corelisters.NewSecretLister(secretInformer.GetIndexer())

	key := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}

	// Create a bucket that contains our key to simulate being leader
	bucket := &fakeBucket{key: key}
	r := &reconciler{
		LeaderAwareFuncs: pkgreconciler.LeaderAwareFuncs{
			PromoteFunc: func(bkt pkgreconciler.Bucket, enq func(pkgreconciler.Bucket, types.NamespacedName)) error {
				return nil
			},
		},
		client:       client,
		secretlister: secretLister,
		key:          key,
		serviceName:  serviceName,
	}
	// Promote to set leader status
	_ = r.Promote(bucket, func(pkgreconciler.Bucket, types.NamespacedName) {})

	// Reconcile should regenerate certificates
	err := r.reconcileCertificate(ctx)
	if err != nil {
		t.Fatalf("reconcileCertificate() = %v, want no error", err)
	}

	// Verify the secret was updated with certificate data
	updatedSecret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}

	// Check that all required keys are present and non-empty
	if updatedSecret.Data == nil {
		t.Error("Secret.Data is nil after reconciliation")
	}
	if len(updatedSecret.Data[certresources.ServerKey]) == 0 {
		t.Error("ServerKey is missing or empty after reconciliation")
	}
	if len(updatedSecret.Data[certresources.ServerCert]) == 0 {
		t.Error("ServerCert is missing or empty after reconciliation")
	}
	if len(updatedSecret.Data[certresources.CACert]) == 0 {
		t.Error("CACert is missing or empty after reconciliation")
	}
}

func TestReconcileCertificateWithEmptyDataMap(t *testing.T) {
	ctx := context.Background()
	namespace := system.Namespace()
	secretName := "webhook-certs"
	serviceName := "webhook"

	// Create a secret with empty Data map (simulating reinstall scenario)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{},
	}

	client := fake.NewSimpleClientset(secret)
	secretInformer := cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "secrets", namespace, nil),
		&corev1.Secret{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	secretInformer.GetStore().Add(secret)
	secretLister := corelisters.NewSecretLister(secretInformer.GetIndexer())

	key := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}

	// Create a bucket that contains our key to simulate being leader
	bucket := &fakeBucket{key: key}
	r := &reconciler{
		LeaderAwareFuncs: pkgreconciler.LeaderAwareFuncs{
			PromoteFunc: func(bkt pkgreconciler.Bucket, enq func(pkgreconciler.Bucket, types.NamespacedName)) error {
				return nil
			},
		},
		client:       client,
		secretlister: secretLister,
		key:          key,
		serviceName:  serviceName,
	}
	// Promote to set leader status
	_ = r.Promote(bucket, func(pkgreconciler.Bucket, types.NamespacedName) {})

	// Reconcile should regenerate certificates
	err := r.reconcileCertificate(ctx)
	if err != nil {
		t.Fatalf("reconcileCertificate() = %v, want no error", err)
	}

	// Verify the secret was updated with certificate data
	updatedSecret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}

	// Check that all required keys are present and non-empty
	if len(updatedSecret.Data) == 0 {
		t.Error("Secret.Data is empty after reconciliation")
	}
	if len(updatedSecret.Data[certresources.ServerKey]) == 0 {
		t.Error("ServerKey is missing or empty after reconciliation")
	}
	if len(updatedSecret.Data[certresources.ServerCert]) == 0 {
		t.Error("ServerCert is missing or empty after reconciliation")
	}
	if len(updatedSecret.Data[certresources.CACert]) == 0 {
		t.Error("CACert is missing or empty after reconciliation")
	}
}

func TestReconcileCertificateWithEmptyKeyValues(t *testing.T) {
	ctx := context.Background()
	namespace := system.Namespace()
	secretName := "webhook-certs"
	serviceName := "webhook"

	// Create a secret with keys but empty values
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			certresources.ServerKey:  {},
			certresources.ServerCert: {},
			certresources.CACert:     {},
		},
	}

	client := fake.NewSimpleClientset(secret)
	secretInformer := cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "secrets", namespace, nil),
		&corev1.Secret{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	secretInformer.GetStore().Add(secret)
	secretLister := corelisters.NewSecretLister(secretInformer.GetIndexer())

	key := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}

	// Create a bucket that contains our key to simulate being leader
	bucket := &fakeBucket{key: key}
	r := &reconciler{
		LeaderAwareFuncs: pkgreconciler.LeaderAwareFuncs{
			PromoteFunc: func(bkt pkgreconciler.Bucket, enq func(pkgreconciler.Bucket, types.NamespacedName)) error {
				return nil
			},
		},
		client:       client,
		secretlister: secretLister,
		key:          key,
		serviceName:  serviceName,
	}
	// Promote to set leader status
	_ = r.Promote(bucket, func(pkgreconciler.Bucket, types.NamespacedName) {})

	// Reconcile should regenerate certificates
	err := r.reconcileCertificate(ctx)
	if err != nil {
		t.Fatalf("reconcileCertificate() = %v, want no error", err)
	}

	// Verify the secret was updated with certificate data
	updatedSecret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}

	// Check that all required keys are present and non-empty
	if len(updatedSecret.Data[certresources.ServerKey]) == 0 {
		t.Error("ServerKey is missing or empty after reconciliation")
	}
	if len(updatedSecret.Data[certresources.ServerCert]) == 0 {
		t.Error("ServerCert is missing or empty after reconciliation")
	}
	if len(updatedSecret.Data[certresources.CACert]) == 0 {
		t.Error("CACert is missing or empty after reconciliation")
	}
}
