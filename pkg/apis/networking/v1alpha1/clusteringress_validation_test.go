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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestClusterIngressValidation(t *testing.T) {
	tests := []struct {
		name string
		ci   *ClusterIngress
		want *apis.FieldError
	}{{
		name: "valid",
		ci: &ClusterIngress{
			Spec: IngressSpec{
				TLS: []IngressTLS{{
					SecretNamespace: "secret-space",
					SecretName:      "secret-name",
				}},
				Rules: []IngressRule{{
					Hosts: []string{"example.com"},
					HTTP: &HTTPIngressRuleValue{
						Paths: []HTTPIngressPath{{
							Splits: []IngressBackendSplit{{
								IngressBackend: IngressBackend{
									ServiceName:      "revision-000",
									ServiceNamespace: "default",
									ServicePort:      intstr.FromInt(8080),
								},
							}},
							Retries: &HTTPRetry{
								Attempts: 3,
							},
						}},
					},
				}},
			},
		},
		want: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.ci.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
