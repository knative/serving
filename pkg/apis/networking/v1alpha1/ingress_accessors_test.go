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

package v1alpha1

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestIngressAccessorMethods(t *testing.T) {
	ci := &Ingress{
		Status: IngressStatus{
			LoadBalancer: &LoadBalancerStatus{
				Ingress: []LoadBalancerIngressStatus{{
					DomainInternal: "test-domain",
				}},
			},
		},
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
	}

	ia := ci
	for i := range ci.Spec.Rules {
		ci.Spec.Rules[i].Hosts = append(ci.Spec.Rules[i].Hosts, "exampletest.com")
	}
	ia.SetSpec(ci.Spec)
	spec := ia.GetSpec()
	if strings.Compare(spec.Rules[0].Hosts[0], "example.com") != 0 && strings.Compare(spec.Rules[0].Hosts[1], "exampletest.com") != 0 {
		t.Error("Failed to call IngressAccessor.getHost()")
	}

	status := ia.GetStatus()
	if strings.Compare(status.LoadBalancer.Ingress[0].DomainInternal, "test-domain") != 0 {
		t.Error("Failed to call IngressAccessor.GetStatus()")
	}

	status2 := status.DeepCopy()
	status2.LoadBalancer.Ingress[0].DomainInternal = "test-domain2"

	ia.SetStatus(*status2)
	status = ia.GetStatus()
	if strings.Compare(status.LoadBalancer.Ingress[0].DomainInternal, "test-domain2") != 0 {
		t.Error("Failed to call IngressAccessor.SetStatus()")
	}
}
