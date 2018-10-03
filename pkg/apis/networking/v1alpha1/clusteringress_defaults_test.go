/*
Copyright 2018 The Knative Authors

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

package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestClusterIngressDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *ClusterIngress
		want *ClusterIngress
	}{{
		name: "empty",
		in:   &ClusterIngress{},
		want: &ClusterIngress{},
	}, {
		name: "tls-defaulting",
		in: &ClusterIngress{
			Spec: ClusterIngressSpec{
				TLS: []ClusterIngressTLS{{
					SecretNamespace: "secret-space",
					SecretName:      "secret-name",
				}},
			},
		},
		want: &ClusterIngress{
			Spec: ClusterIngressSpec{
				TLS: []ClusterIngressTLS{{
					SecretNamespace: "secret-space",
					SecretName:      "secret-name",
					// Default secret keys are filled in.
					ServerCertificate: "tls.cert",
					PrivateKey:        "tls.key",
				}},
			},
		},
	}, {
		name: "split-defaulting",
		in: &ClusterIngress{
			Spec: ClusterIngressSpec{
				Rules: []ClusterIngressRule{{
					ClusterIngressRuleValue: ClusterIngressRuleValue{
						HTTP: &HTTPClusterIngressRuleValue{
							Paths: []HTTPClusterIngressPath{{
								Splits: []ClusterIngressBackendSplit{{
									Backend: &ClusterIngressBackend{
										ServiceName:      "revision-000",
										ServiceNamespace: "default",
										ServicePort:      intstr.FromInt(8080),
									},
								}},
							}},
						},
					},
				}},
			},
		},
		want: &ClusterIngress{
			Spec: ClusterIngressSpec{
				Rules: []ClusterIngressRule{{
					ClusterIngressRuleValue: ClusterIngressRuleValue{
						HTTP: &HTTPClusterIngressRuleValue{
							Paths: []HTTPClusterIngressPath{{
								Splits: []ClusterIngressBackendSplit{{
									Backend: &ClusterIngressBackend{
										ServiceName:      "revision-000",
										ServiceNamespace: "default",
										ServicePort:      intstr.FromInt(8080),
									},
									// Percent is filled in.
									Percent: 100,
								}},
							}},
						},
					},
				}},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			got.SetDefaults()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}

}
