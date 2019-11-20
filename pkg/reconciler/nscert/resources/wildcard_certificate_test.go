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

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler/nscert/resources/names"
)

var namespace = &corev1.Namespace{
	ObjectMeta: metav1.ObjectMeta{
		Name: "testns",
	},
}

const (
	domain  = "example.com"
	dnsName = "*.testns.example.com"
)

func TestMakeWildcardCertificate(t *testing.T) {
	want := &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.WildcardCertificate(dnsName),
			Namespace:       "testns",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(namespace, corev1.SchemeGroupVersion.WithKind("Namespace"))},
			Labels: map[string]string{
				networking.WildcardCertDomainLabelKey: domain,
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{dnsName},
			SecretName: names.WildcardCertificate(dnsName),
		},
	}

	got := MakeWildcardCertificate(namespace, dnsName, domain)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeWildcardCertificate (-want, +got) = %s", diff)
	}
}
