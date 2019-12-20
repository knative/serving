// +build e2e

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
	"net/http"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

// TestBasics verifies that a no frills Ingress exposes a simple Pod/Service via the public load balancer.
func TestBasics(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	name, port, cancel := CreateService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()
	test.CleanupOnInterrupt(cancel)

	// Create a simple Ingress over the Service.
	ing := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Annotations: map[string]string{
				networking.IngressClassAnnotationKey: test.ServingFlags.IngressClass,
			},
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts:      []string{name + ".example.com"},
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
		},
	}
	ing, err := clients.NetworkingClient.Ingresses.Create(ing)
	if err != nil {
		t.Fatalf("Error creating Ingress: %v", err)
	}
	defer clients.NetworkingClient.Ingresses.Delete(ing.Name, &metav1.DeleteOptions{})
	test.CleanupOnInterrupt(func() { clients.NetworkingClient.Ingresses.Delete(ing.Name, &metav1.DeleteOptions{}) })

	if err := v1a1test.WaitForIngressState(clients.NetworkingClient, ing.Name, v1a1test.IsIngressReady, t.Name()); err != nil {
		t.Fatalf("Error waiting for ingress state: %v", err)
	}
	ing, err = clients.NetworkingClient.Ingresses.Get(name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error getting Ingress: %v", err)
	}

	// Create a client with a dialer based on the Ingress' public load balancer.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: CreateDialContext(t, ing, clients),
		},
	}

	// Make a request and check the response.
	resp, err := client.Get("http://" + name + ".example.com")
	if err != nil {
		t.Fatalf("Error creating Ingress: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Got non-OK status: %d", resp.StatusCode)
	}
}
