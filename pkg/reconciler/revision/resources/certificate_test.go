/*
Copyright 2023 The Knative Authors

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

package resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/certificates"
	"knative.dev/networking/pkg/config"
	servingnetworking "knative.dev/serving/pkg/networking"
)

const (
	namespace = "test-ns"
	domain    = "example.com"
	dnsName   = "kn-user-test-ns"
)

func TestMakeQueueProxyCertificate(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}

	want := &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            servingnetworking.ServingCertName,
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ns, corev1.SchemeGroupVersion.WithKind("Namespace"))},
			Annotations: map[string]string{
				networking.CertificateClassAnnotationKey: config.CertManagerCertificateClassName,
			},
			Labels: map[string]string{
				networking.CertificateTypeLabelKey: string(config.CertificateSystemInternal),
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{dnsName, certificates.LegacyFakeDnsName},
			SecretName: servingnetworking.ServingCertName,
		},
	}

	got := MakeQueueProxyCertificate(ns, config.CertManagerCertificateClassName)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("MakeQueueProxyCertificate (-want, +got) =", diff)
	}
}
