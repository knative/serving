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

package resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkingv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	// . "knative.dev/serving/pkg/testing/v1"
)

func TestMakeCertificate(t *testing.T) {
	certClass := "cert-class"
	for _, tc := range []struct {
		name string
		dm   v1alpha1.DomainMapping
		want networkingv1alpha1.Certificate
	}{{
		name: "basic",
		dm: v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
			},
			Spec: v1alpha1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "the-namespace",
					Name:      "the-name",
				},
			},
		},
		want: networkingv1alpha1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "mapping.com",
				Namespace:   "the-namespace",
				Annotations: map[string]string{"networking.knative.dev/certificate.class": certClass},
				Labels: map[string]string{
					serving.DomainMappingUIDLabelKey: "mapping.com",
				},
			},
			Spec: networkingv1alpha1.CertificateSpec{
				DNSNames: []string{
					"mapping.com",
				},
				SecretName: "mapping.com",
			},
		},
	}, {
		name: "reference existing tls secret",
		dm: v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
			},
			Spec: v1alpha1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "the-namespace",
					Name:      "the-name",
				},
				TLS: &v1alpha1.SecretTLS{
					SecretName: "existing-secret",
				},
			},
		},
		want: networkingv1alpha1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "mapping.com",
				Namespace:   "the-namespace",
				Annotations: map[string]string{"networking.knative.dev/certificate.class": certClass},
				Labels: map[string]string{
					serving.DomainMappingLabelKey: "mapping.com",
				},
			},
			Spec: networkingv1alpha1.CertificateSpec{
				DNSNames: []string{
					"mapping.com",
				},
				SecretName: "existing-secret",
			},
		},
	}, {
		name: "filter last-applied",
		dm: v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					corev1.LastAppliedConfigAnnotation: "filtered",
					"others":                           "kept",
				},
			},
			Spec: v1alpha1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "the-namespace",
					Name:      "the-name",
				},
			},
		},
		want: networkingv1alpha1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					"networking.knative.dev/certificate.class": certClass,
					"others": "kept",
				},
				Labels: map[string]string{
					serving.DomainMappingUIDLabelKey: "mapping.com",
				},
			},
			Spec: networkingv1alpha1.CertificateSpec{
				DNSNames: []string{
					"mapping.com",
				},
				SecretName: "mapping.com",
			},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			tc.want.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(&tc.dm)}
			got := *MakeCertificate(&tc.dm, certClass)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Unexpected certificate (-want, +got):\n%v", diff)
			}
		})
	}
}
