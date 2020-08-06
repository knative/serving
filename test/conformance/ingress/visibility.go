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
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/pool"
	"knative.dev/serving/test"
)

func TestVisibility(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	// Create the private backend
	name, port, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	privateServiceName := test.ObjectNameForTest(t)
	privateHostName := privateServiceName + "." + test.ServingNamespace + ".svc.cluster.local"
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
	RuntimeRequestWithExpectations(t, client, "http://"+privateHostName, []ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusNotFound))}, true)

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

func TestVisibilitySplit(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	// Use a post-split injected header to establish which split we are sending traffic to.
	const headerName = "Foo-Bar-Baz"

	backends := make([]v1alpha1.IngressBackendSplit, 0, 10)
	weights := make(map[string]float64, len(backends))

	// Double the percentage of the split each iteration until it would overflow, and then
	// give the last route the remainder.
	percent, total := 1, 0
	for i := 0; i < 10; i++ {
		name, port, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
		defer cancel()
		backends = append(backends, v1alpha1.IngressBackendSplit{
			IngressBackend: v1alpha1.IngressBackend{
				ServiceName:      name,
				ServiceNamespace: test.ServingNamespace,
				ServicePort:      intstr.FromInt(port),
			},
			// Append different headers to each split, which lets us identify
			// which backend we hit.
			AppendHeaders: map[string]string{
				headerName: name,
			},
			Percent: percent,
		})
		weights[name] = float64(percent)

		total += percent
		percent *= 2
		// Cap the final non-zero bucket so that we total 100%
		// After that, this will zero out remaining buckets.
		if total+percent > 100 {
			percent = 100 - total
		}
	}

	name := test.ObjectNameForTest(t)

	// Create a simple Ingress over the 10 Services.
	privateHostName := fmt.Sprintf("%s.%s.%s", name, test.ServingNamespace, "svc.cluster.local")
	localIngress, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{privateHostName},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: backends,
				}},
			},
		}},
	})
	defer cancel()

	// Ensure we can't connect to the private resources
	RuntimeRequestWithExpectations(t, client, "http://"+privateHostName, []ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusNotFound))}, true)

	loadbalancerAddress := localIngress.Status.PrivateLoadBalancer.Ingress[0].DomainInternal
	proxyName, proxyPort, cancel := CreateProxyService(t, clients, privateHostName, loadbalancerAddress)
	defer cancel()

	publicHostName := fmt.Sprintf("%s.%s", name, "example.com")
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

	// Create a large enough population of requests that we can reasonably assess how
	// well the Ingress respected the percentage split.
	seen := make(map[string]float64, len(backends))

	const (
		// The total number of requests to make (as a float to avoid conversions in later computations).
		totalRequests = 1000.0
		// The increment to make for each request, so that the values of seen reflect the
		// percentage of the total number of requests we are making.
		increment = 100.0 / totalRequests
		// Allow the Ingress to be within 10% of the configured value.
		margin = 10.0
	)
	wg := pool.New(8)
	resultCh := make(chan string, totalRequests)

	for i := 0.0; i < totalRequests; i++ {
		wg.Go(func() error {
			ri := RuntimeRequest(t, client, "http://"+publicHostName)
			if ri == nil {
				return errors.New("failed to request")
			}
			resultCh <- ri.Request.Headers.Get(headerName)
			return nil
		})
	}
	if err := wg.Wait(); err != nil {
		t.Errorf("Error while sending requests: %v", err)
	}
	close(resultCh)

	for r := range resultCh {
		seen[r] += increment
	}

	for name, want := range weights {
		got := seen[name]
		switch {
		case want == 0.0 && got > 0.0:
			// For 0% targets, we have tighter requirements.
			t.Errorf("Target %q received traffic, wanted none (0%% target).", name)
		case math.Abs(got-want) > margin:
			t.Errorf("Target %q received %f%%, wanted %f +/- %f", name, got, want, margin)
		}
	}
}

func TestVisibilityPath(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	// For /foo
	fooName, fooPort, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	// For /bar
	barName, barPort, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	// For /baz
	bazName, bazPort, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	mainName, port, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	// Use a post-split injected header to establish which split we are sending traffic to.
	const headerName = "Which-Backend"

	name := test.ObjectNameForTest(t)
	privateHostName := fmt.Sprintf("%s.%s.%s", name, test.ServingNamespace, "svc.cluster.local")
	localIngress, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{privateHostName},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Path: "/foo",
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      fooName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(fooPort),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: fooName,
						},
						Percent: 100,
					}},
				}, {
					Path: "/bar",
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      barName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(barPort),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: barName,
						},
						Percent: 100,
					}},
				}, {
					Path: "/baz",
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      bazName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(bazPort),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: bazName,
						},
						Percent: 100,
					}},
				}, {
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      mainName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(port),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: mainName,
						},
						Percent: 100,
					}},
				}},
			},
		}},
	})
	defer cancel()

	// Ensure we can't connect to the private resources
	for _, path := range []string{"", "/foo", "/bar", "/baz"} {
		RuntimeRequestWithExpectations(t, client, "http://"+privateHostName+path, []ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusNotFound))}, true)
	}

	loadbalancerAddress := localIngress.Status.PrivateLoadBalancer.Ingress[0].DomainInternal
	proxyName, proxyPort, cancel := CreateProxyService(t, clients, privateHostName, loadbalancerAddress)
	defer cancel()

	publicHostName := fmt.Sprintf("%s.%s", name, "example.com")
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

	tests := map[string]string{
		"/foo":  fooName,
		"/bar":  barName,
		"/baz":  bazName,
		"":      mainName,
		"/asdf": mainName,
	}

	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			ri := RuntimeRequest(t, client, "http://"+publicHostName+path)
			if ri == nil {
				return
			}

			got := ri.Request.Headers.Get(headerName)
			if got != want {
				t.Errorf("Header[%q] = %q, wanted %q", headerName, got, want)
			}
		})
	}
}
