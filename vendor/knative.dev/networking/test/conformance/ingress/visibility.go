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
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/pkg/pool"
)

func TestVisibility(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	// Create the private backend
	name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	privateServiceName := test.ObjectNameForTest(t)
	shortName := privateServiceName + "." + test.ServingNamespace

	var privateHostNames = map[string]string{
		"fqdn":     shortName + ".svc." + test.NetworkingFlags.ClusterSuffix,
		"short":    shortName + ".svc",
		"shortest": shortName,
	}
	ingress, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{privateHostNames["fqdn"], privateHostNames["short"], privateHostNames["shortest"]},
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

	// Ensure the service is not publicly accessible
	for _, privateHostName := range privateHostNames {
		RuntimeRequestWithExpectations(ctx, t, client, "http://"+privateHostName, []ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusNotFound))}, true)
	}

	for name, privateHostName := range privateHostNames {
		t.Run(name, func(t *testing.T) {
			testProxyToHelloworld(ctx, t, ingress, clients, privateHostName)
		})
	}
}

func testProxyToHelloworld(ctx context.Context, t *testing.T, ingress *v1alpha1.Ingress, clients *test.Clients, privateHostName string) {

	loadbalancerAddress := ingress.Status.PrivateLoadBalancer.Ingress[0].DomainInternal
	proxyName, proxyPort, _ := CreateProxyService(ctx, t, clients, privateHostName, loadbalancerAddress)

	// Using fixed hostnames can lead to conflicts when -count=N>1
	// so pseudo-randomize the hostnames to avoid conflicts.
	publicHostName := test.ObjectNameForTest(t) + ".publicproxy.example.com"

	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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

	// Ensure the service is accessible from within the cluster.
	RuntimeRequest(ctx, t, client, "http://"+publicHostName)
}

func TestVisibilitySplit(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	// Use a post-split injected header to establish which split we are sending traffic to.
	const headerName = "Foo-Bar-Baz"

	backends := make([]v1alpha1.IngressBackendSplit, 0, 10)
	weights := make(map[string]float64, len(backends))

	// Double the percentage of the split each iteration until it would overflow, and then
	// give the last route the remainder.
	percent, total := 1, 0
	for i := 0; i < 10; i++ {
		name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)
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
	privateHostName := fmt.Sprintf("%s.%s.svc.%s", name, test.ServingNamespace, test.NetworkingFlags.ClusterSuffix)
	localIngress, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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

	// Ensure we can't connect to the private resources
	RuntimeRequestWithExpectations(ctx, t, client, "http://"+privateHostName, []ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusNotFound))}, true)

	loadbalancerAddress := localIngress.Status.PrivateLoadBalancer.Ingress[0].DomainInternal
	proxyName, proxyPort, _ := CreateProxyService(ctx, t, clients, privateHostName, loadbalancerAddress)

	publicHostName := fmt.Sprintf("%s.%s", name, "example.com")
	_, client, _ = CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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
	wg := pool.NewWithCapacity(8, totalRequests)
	resultCh := make(chan string, totalRequests)

	for i := 0.0; i < totalRequests; i++ {
		wg.Go(func() error {
			ri := RuntimeRequest(ctx, t, client, "http://"+publicHostName)
			if ri == nil {
				return errors.New("failed to request")
			}
			resultCh <- ri.Request.Headers.Get(headerName)
			return nil
		})
	}
	if err := wg.Wait(); err != nil {
		t.Error("Error while sending requests:", err)
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
	ctx, clients := context.Background(), test.Setup(t)

	// For /foo
	fooName, fooPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// For /bar
	barName, barPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// For /baz
	bazName, bazPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	mainName, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// Use a post-split injected header to establish which split we are sending traffic to.
	const headerName = "Which-Backend"

	name := test.ObjectNameForTest(t)
	privateHostName := fmt.Sprintf("%s.%s.svc.%s", name, test.ServingNamespace, test.NetworkingFlags.ClusterSuffix)
	localIngress, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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

	// Ensure we can't connect to the private resources
	for _, path := range []string{"", "/foo", "/bar", "/baz"} {
		RuntimeRequestWithExpectations(ctx, t, client, "http://"+privateHostName+path, []ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusNotFound))}, true)
	}

	loadbalancerAddress := localIngress.Status.PrivateLoadBalancer.Ingress[0].DomainInternal
	proxyName, proxyPort, _ := CreateProxyService(ctx, t, clients, privateHostName, loadbalancerAddress)

	publicHostName := fmt.Sprintf("%s.%s", name, "example.com")
	_, client, _ = CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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

	tests := map[string]string{
		"/foo":  fooName,
		"/bar":  barName,
		"/baz":  bazName,
		"":      mainName,
		"/asdf": mainName,
	}

	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			ri := RuntimeRequest(ctx, t, client, "http://"+publicHostName+path)
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
