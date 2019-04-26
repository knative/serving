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
	"testing"

	"github.com/knative/pkg/system"

	"github.com/google/go-cmp/cmp"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var route = &v1alpha1.Route{
	ObjectMeta: metav1.ObjectMeta{
		Name: "route",
		UID:  "12345",
	},
}

var dnsNames = []string{"v1.default.example.com", "subroute.v1.default.example.com"}

func TestMakeCertificates(t *testing.T) {
	tests := []struct {
		name           string
		enableWildcard bool
		want           []*netv1alpha1.Certificate
	}{{
		name:           "wildcard certificate",
		enableWildcard: true,
		want: []*netv1alpha1.Certificate{
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default.example.com",
					Namespace: system.Namespace(),
				},
				Spec: netv1alpha1.CertificateSpec{
					DNSNames:   []string{"*.default.example.com"},
					SecretName: "default.example.com",
				},
			},
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v1.default.example.com",
					Namespace: system.Namespace(),
				},
				Spec: netv1alpha1.CertificateSpec{
					DNSNames:   []string{"*.v1.default.example.com"},
					SecretName: "v1.default.example.com",
				},
			},
		},
	}, {
		name:           "non-wildcard certificate",
		enableWildcard: false,
		want: []*netv1alpha1.Certificate{
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%s", route.Name, route.UID),
					Namespace: system.Namespace(),
				},
				Spec: netv1alpha1.CertificateSpec{
					DNSNames:   dnsNames,
					SecretName: fmt.Sprintf("%s-%s", route.Name, route.UID),
				},
			},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeCertificates(route, dnsNames, test.enableWildcard)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeCertificates (-want, +got) = %v", diff)
			}
		})
	}
}
