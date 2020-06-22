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
package resources

import (
	"fmt"
	"hash/adler32"
	"strconv"
	"testing"

	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"

	. "knative.dev/serving/pkg/testing/v1"
)

var (
	dnsNameTagMap = map[string]string{
		"v1.default.example.com":         "",
		"v1-current.default.example.com": "current",
	}

	fakeUID         = types.UID("12-34")
	fakeCertName    = "route-" + strconv.Itoa(int(adler32.Checksum([]byte(fakeUID))))
	fakeTagChecksum = "200999684"

	route = Route("default", "route", WithRouteUID(fakeUID))
)

func TestMakeCertificates(t *testing.T) {
	want := []*netv1alpha1.Certificate{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("%s-%s", fakeCertName, fakeTagChecksum),
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "foo-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey: "route",
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1-current.default.example.com"},
				SecretName: fmt.Sprintf("%s-%s", fakeCertName, fakeTagChecksum),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fakeCertName,
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "foo-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey: "route",
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1.default.example.com"},
				SecretName: fakeCertName,
			},
		},
	}
	got := MakeCertificates(route, dnsNameTagMap, "foo-cert")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertificate (-want, +got) = %v", diff)
	}
}

func TestMakeCertificates_FilterLastAppliedAnno(t *testing.T) {
	var orgRoute = Route("default", "route", WithRouteUID(fakeUID), WithRouteLabel(map[string]string{"label-from-route": "foo", serving.RouteLabelKey: "foo"}),
		WithRouteAnnotation(map[string]string{corev1.LastAppliedConfigAnnotation: "something-last-applied", networking.CertificateClassAnnotationKey: "passdown-cert"}))
	want := []*netv1alpha1.Certificate{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("%s-%s", fakeCertName, fakeTagChecksum),
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(orgRoute)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "passdown-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey: "route",
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1-current.default.example.com"},
				SecretName: fmt.Sprintf("%s-%s", fakeCertName, fakeTagChecksum),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fakeCertName,
				Namespace:       "default",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(orgRoute)},
				Annotations: map[string]string{
					networking.CertificateClassAnnotationKey: "passdown-cert",
				},
				Labels: map[string]string{
					serving.RouteLabelKey: "route",
				},
			},
			Spec: netv1alpha1.CertificateSpec{
				DNSNames:   []string{"v1.default.example.com"},
				SecretName: fakeCertName,
			},
		},
	}
	got := MakeCertificates(orgRoute, dnsNameTagMap, "default-cert")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertificate (-want, +got) = %v", diff)
	}
}
