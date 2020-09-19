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

package networking

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	netclientset "knative.dev/networking/pkg/client/clientset/versioned"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakecertinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	listers "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
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
	netclient  netclientset.Interface
	certLister listers.CertificateLister
}

func (f *FakeAccessor) GetNetworkingClient() netclientset.Interface {
	return f.netclient
}

func (f *FakeAccessor) GetCertificateLister() listers.CertificateLister {
	return f.certLister
}

func TestReconcileCertificateCreate(t *testing.T) {
	ctx, accessor, done := setup([]*v1alpha1.Certificate{}, t)
	defer done()

	ReconcileCertificate(ctx, ownerObj, desired, accessor)

	certInformer := fakecertinformer.Get(ctx)
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		cert, err := certInformer.Lister().Certificates(desired.Namespace).Get(desired.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return cmp.Equal(cert, desired), nil
	}); err != nil {
		t.Error("Failed seeing creation of cert:", err)
	}
}

func TestReconcileCertificateUpdate(t *testing.T) {
	ctx, accessor, done := setup([]*v1alpha1.Certificate{origin}, t)
	defer done()

	ReconcileCertificate(ctx, ownerObj, desired, accessor)
	certInformer := fakecertinformer.Get(ctx)
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		cert, err := certInformer.Lister().Certificates(desired.Namespace).Get(desired.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return cmp.Equal(cert, desired), nil
	}); err != nil {
		t.Error("Failed seeing creation of cert:", err)
	}
}

func setup(certs []*v1alpha1.Certificate, t *testing.T) (context.Context, *FakeAccessor, func()) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	fake := fakenetworkingclient.Get(ctx)
	certInformer := fakecertinformer.Get(ctx)

	for _, cert := range certs {
		fake.NetworkingV1alpha1().Certificates(cert.Namespace).Create(ctx, cert, metav1.CreateOptions{})
		certInformer.Informer().GetIndexer().Add(cert)
	}

	waitInformers, err := controller.RunInformers(ctx.Done(), certInformer.Informer())
	if err != nil {
		t.Fatal("failed to start Certificate informer:", err)
	}

	return ctx, &FakeAccessor{
			netclient:  fake,
			certLister: certInformer.Lister(),
		}, func() {
			cancel()
			waitInformers()
		}
}
