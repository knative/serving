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
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/types"
)

// Header to disambiguate what version we're talking to.
const updateHeaderName = "Who-Are-You"

// TestUpdate verifies that when the network programming changes that traffic isn't dropped.
func TestUpdate(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	firstName, firstPort, firstCancel := CreateService(t, clients, networking.ServicePortNameHTTP1)

	// Create a simple Ingress over the Service.
	hostname := test.ObjectNameForTest(t)
	name, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
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
	defer cancel()

	proberCancel := checkOK(t, "http://"+hostname+".example.com", client)

	// Give the prober a chance to get started.
	time.Sleep(1 * time.Second)

	previousVersionCancel := func() {
		t.Logf("Tearing down %q", firstName)
		firstCancel()
	}
	for i := 0; i < 2; i++ {
		nextName, nextPort, nextCancel := CreateService(t, clients, networking.ServicePortNameHTTP1)

		t.Logf("Rolling out %q", nextName)

		// Update the Ingress, and wait for it to report ready.
		UpdateIngressReady(t, clients, name, v1alpha1.IngressSpec{
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
							// Append different headers to each split, which lets us identify
							// which backend we hit.
							AppendHeaders: map[string]string{
								updateHeaderName: nextName,
							},
						}},
					}},
				},
			}},
		})

		// Check that it serves the right message as soon as we get "Ready",
		// but before we stop probing.
		resp, err := client.Get("http://" + hostname + ".example.com")
		if err != nil {
			t.Fatalf("Error making GET request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Got non-OK status: %d", resp.StatusCode)
		}
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Errorf("Unable to read response body: %v", err)
		}
		ri := &types.RuntimeInfo{}
		if err := json.Unmarshal(b, ri); err != nil {
			t.Fatalf("Unable to parse runtime image's response payload: %v", err)
		}
		if got := ri.Request.Headers.Get(updateHeaderName); got != nextName {
			t.Errorf("Header[%q] = %q, wanted %q", updateHeaderName, got, nextName)
		}

		// Once we've rolled out, cancel the previous version.
		previousVersionCancel()
		// Then make ourselves the next to be cancelled.
		previousVersionCancel = func() {
			t.Logf("Tearing down %q", nextName)
			nextCancel()
		}
	}

	// Stop the prober.
	proberCancel()
	// Then cleanup the final version.
	previousVersionCancel()
}

func checkOK(t *testing.T, url string, client *http.Client) context.CancelFunc {
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
				// Make a request and check the response.
				resp, err := client.Get(url)
				if err != nil {
					t.Fatalf("Error making GET request: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Got non-OK status: %d", resp.StatusCode)
				} else {
					b, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						t.Errorf("Unable to read response body: %v", err)
					}
					ri := &types.RuntimeInfo{}
					if err := json.Unmarshal(b, ri); err != nil {
						t.Fatalf("Unable to parse runtime image's response payload: %v", err)
					}

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
