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
	"context"
	"net/http"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
)

// Header to disambiguate what version we're talking to.
const updateHeaderName = "Who-Are-You"

// TestUpdate verifies that when the network programming changes that traffic isn't dropped.
func TestUpdate(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	firstName, firstPort, firstCancel := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// Create a simple Ingress over the Service.
	hostname := test.ObjectNameForTest(t)
	ing, client, cancel := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{hostname + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      firstName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(firstPort),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							updateHeaderName: firstName,
						},
					}},
				}},
			},
		}},
	})

	previousVersionCancel := func() {
		t.Logf("Tearing down %q", firstName)
		firstCancel()
	}

	proberCancel := checkOK(ctx, t, "http://"+hostname+".example.com", client)
	defer func() {
		proberCancel()
		previousVersionCancel()
		cancel()
	}()

	// Give the prober a chance to get started.
	time.Sleep(1 * time.Second)

	// First test with only sentinel changes.
	for i := 0; i < 10; i++ {
		sentinel := test.ObjectNameForTest(t)

		t.Logf("Rolling out %q w/ %q", firstName, sentinel)

		// Update the Ingress, and wait for it to report ready.
		UpdateIngressReady(ctx, t, clients, ing.Name, v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts:      []string{hostname + ".example.com"},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceName:      firstName,
								ServiceNamespace: test.ServingNamespace,
								ServicePort:      intstr.FromInt(firstPort),
							},
							AppendHeaders: map[string]string{
								updateHeaderName: sentinel,
							},
						}},
					}},
				},
			}},
		})

		// Check that it serves the right message as soon as we get "Ready",
		// but before we stop probing.
		ri := RuntimeRequest(ctx, t, client, "http://"+hostname+".example.com")
		if ri != nil {
			if got := ri.Request.Headers.Get(updateHeaderName); got != sentinel {
				t.Errorf("Header[%q] = %q, wanted %q", updateHeaderName, got, sentinel)
			}
		}
	}

	// Next test with varying sentinels AND fresh services each time.
	previousVersionCancel = func() {
		t.Logf("Tearing down %q", firstName)
		firstCancel()
	}
	for i := 0; i < 10; i++ {
		sentinel := test.ObjectNameForTest(t)
		nextName, nextPort, nextCancel := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

		t.Logf("Rolling out %q w/ %q", nextName, sentinel)

		// Update the Ingress, and wait for it to report ready.
		UpdateIngressReady(ctx, t, clients, ing.Name, v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts:      []string{hostname + ".example.com"},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceName:      nextName,
								ServiceNamespace: test.ServingNamespace,
								ServicePort:      intstr.FromInt(nextPort),
							},
							AppendHeaders: map[string]string{
								updateHeaderName: sentinel,
							},
						}},
					}},
				},
			}},
		})

		// Check that it serves the right message as soon as we get "Ready",
		// but before we stop probing.
		ri := RuntimeRequest(ctx, t, client, "http://"+hostname+".example.com")
		if ri != nil {
			if got := ri.Request.Headers.Get(updateHeaderName); got != sentinel {
				t.Errorf("Header[%q] = %q, wanted %q", updateHeaderName, got, sentinel)
			}
		}

		// Once we've rolled out, cancel the previous version.
		previousVersionCancel()
		// Then make ourselves the next to be cancelled.
		previousVersionCancel = func() {
			t.Logf("Tearing down %q", nextName)
			nextCancel()
		}
	}
}

func checkOK(ctx context.Context, t *testing.T, url string, client *http.Client) context.CancelFunc {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	// Launch the prober
	go func() {
		defer close(doneCh)
		for {
			// Each iteration check for cancellation.
			select {
			case <-stopCh:
				return
			default:
			}
			// Scope the defer below to avoid leaking until the test completes.
			func() {
				ri := RuntimeRequest(ctx, t, client, url)
				if ri != nil {
					// Use the updateHeaderName as a debug marker to identify which version
					// (of programming) is responding.
					t.Logf("[%s] Got OK status!", ri.Request.Headers.Get(updateHeaderName))
				}
			}()
		}
	}()

	// Return a cancel function that stops the prober and then waits for it to complete.
	return func() {
		close(stopCh)
		<-doneCh
	}
}
