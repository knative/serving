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

package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/config"
	"knative.dev/serving/pkg/apis/serving"

	"knative.dev/pkg/kmeta"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"

	. "knative.dev/serving/pkg/testing/v1"
)

var (
	dnsNameTagMap = map[string]string{
		"v1.default.example.com":         "",
		"v1-current.default.example.com": "current",
	}
	domain       = "example.com"
	localDomains = sets.New("hello.namespace", "hello.namespace.svc", "hello.namespace.svc.cluster.local")
	route        = Route("default", "route", WithRouteUID("12345"))
)

func TestMakeCertificates(t *testing.T) {
	want := []*netv1alpha1.Certificate{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "route-12345-200999684",
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "foo-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey:              "route",
					networking.CertificateTypeLabelKey: string(config.CertificateExternalDomain),
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1-current.default.example.com"},
				Domain:     domain,
				SecretName: "route-12345-200999684",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "route-12345",
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "foo-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey:              "route",
					networking.CertificateTypeLabelKey: string(config.CertificateExternalDomain),
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1.default.example.com"},
				Domain:     domain,
				SecretName: "route-12345",
			},
		},
	}
	got := MakeCertificates(route, dnsNameTagMap, "foo-cert", domain)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("MakeCertificate (-want, +got) =", diff)
	}
}

func TestMakeCertificates_FilterLastAppliedAnno(t *testing.T) {
	var orgRoute = Route("default", "route", WithRouteUID("12345"), WithRouteLabel(map[string]string{"label-from-route": "foo", serving.RouteLabelKey: "foo"}),
		WithRouteAnnotation(map[string]string{corev1.LastAppliedConfigAnnotation: "something-last-applied", networking.CertificateClassAnnotationKey: "passdown-cert"}))
	want := []*netv1alpha1.Certificate{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "route-12345-200999684",
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(orgRoute)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "passdown-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey:              "route",
					networking.CertificateTypeLabelKey: string(config.CertificateExternalDomain),
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1-current.default.example.com"},
				Domain:     domain,
				SecretName: "route-12345-200999684",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "route-12345",
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(orgRoute)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "passdown-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey:              "route",
					networking.CertificateTypeLabelKey: string(config.CertificateExternalDomain),
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1.default.example.com"},
				Domain:     domain,
				SecretName: "route-12345",
			},
		},
	}
	got := MakeCertificates(orgRoute, dnsNameTagMap, "default-cert", domain)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("MakeCertificate (-want, +got) =", diff)
	}
}

func TestMakeClusterLocalCertificateNoTag(t *testing.T) {
	want := &netv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "route-12345-local",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
			Annotations: map[string]string{
				networking.CertificateClassAnnotationKey: "cert-class",
			},
			Labels: map[string]string{
				serving.RouteLabelKey:              "route",
				networking.CertificateTypeLabelKey: string(config.CertificateClusterLocalDomain),
			},
		},
		Spec: netv1alpha1.CertificateSpec{
			DNSNames:   sets.List(localDomains),
			Domain:     "svc.cluster.local",
			SecretName: "route-12345-local",
		},
	}
	got := MakeClusterLocalCertificate(route, "", localDomains, "cert-class")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("MakeCertificate (-want, +got) =", diff)
	}
}

func TestMakeClusterLocalCertificateWithTag(t *testing.T) {
	want := &netv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "route-12345-73204161-local",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
			Annotations: map[string]string{
				networking.CertificateClassAnnotationKey: "cert-class",
			},
			Labels: map[string]string{
				serving.RouteLabelKey:              "route",
				networking.CertificateTypeLabelKey: string(config.CertificateClusterLocalDomain),
			},
		},
		Spec: netv1alpha1.CertificateSpec{
			DNSNames:   sets.List(localDomains),
			Domain:     "svc.cluster.local",
			SecretName: "route-12345-73204161-local",
		},
	}
	got := MakeClusterLocalCertificate(route, "test", localDomains, "cert-class")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("MakeCertificate (-want, +got) =", diff)
	}
}
