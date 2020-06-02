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

package ingress

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
)

// TestIngressTLS verifies that the Ingress properly handles the TLS field.
func TestIngressTLS(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	name, port, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	hosts := []string{name + ".example.com"}

	secretName, cancel := CreateTLSSecret(t, clients, hosts)
	defer cancel()

	_, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      hosts,
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      name,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(port),
						},
					}},
				}},
			},
		}},
		TLS: []v1alpha1.IngressTLS{{
			Hosts:           hosts,
			SecretName:      secretName,
			SecretNamespace: test.ServingNamespace,
		}},
	})
	defer cancel()

	t.Run("verify HTTP", func(t *testing.T) {
		RuntimeRequest(t, client, "http://"+name+".example.com")
	})

	t.Run("verify HTTPS", func(t *testing.T) {
		RuntimeRequest(t, client, "https://"+name+".example.com")
	})
}

// TODO(mattmoor): Consider adding variants where we have multiple hosts with distinct certificates.
