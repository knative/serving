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
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/test"
)

// TestMultipleHosts verifies that an Ingress can respond to multiple hosts.
func TestMultipleHosts(t *testing.T) {
	t.Parallel()
	defer logstream.Start(t)()
	clients := test.Setup(t)

	name, port, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	// TODO(mattmoor): Once .svc.cluster.local stops being a special case
	// for Visibility, add it here.
	hosts := []string{
		"foo.com",
		"www.foo.com",
		"a-b-1.something-really-really-long.knative.dev",
		"add.your.interesting.domain.here.io",
	}

	// Create a simple Ingress over the Service.
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
	})
	defer cancel()

	for _, host := range hosts {
		RuntimeRequest(t, client, "http://"+host)
	}
}
