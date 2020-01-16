// +build e2e

/*
Copyright 2020 The Knative Authors

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

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
)

func TestVisibility(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	// Create the private backend
	name, port, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	privateHostName := "private." + test.ServingNamespace + ".svc.cluster.local"
	ingress, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{privateHostName},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
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

	// Ensure the service is not publicly accessible
	RuntimeRequestWithStatus(t, client, "http://"+privateHostName, sets.NewInt(http.StatusNotFound))

	loadbalancerAddress := ingress.Status.PrivateLoadBalancer.Ingress[0].DomainInternal
	proxyName, proxyPort, cancel := CreateProxyService(t, clients, privateHostName, loadbalancerAddress)
	defer cancel()

	publicHostName := "publicproxy.example.com"
	_, client, cancel = CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{publicHostName},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      proxyName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(proxyPort),
						},
					}},
				}},
			},
		}},
	})
	defer cancel()

	// Ensure the service is accessible from within the cluster.
	RuntimeRequest(t, client, "http://"+publicHostName)
}
