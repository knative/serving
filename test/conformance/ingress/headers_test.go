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
	"encoding/json"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/types"
)

// TestPreSplitSetHeaders verifies that an Ingress that specified AppendHeaders pre-split has the appropriate header(s) set.
func TestPreSplitSetHeaders(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	name, port, cancel := CreateRuntimeService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	const headerName = "Foo-Bar-Baz"

	// Create a simple Ingress over the 10 Services.
	_, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{name + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					AppendHeaders: map[string]string{
						headerName: name,
					},
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

	t.Run("Check without passing header", func(t *testing.T) {
		// Make a request and check the response.
		resp, err := client.Get("http://" + name + ".example.com")
		if err != nil {
			t.Fatalf("Error making GET request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Got non-OK status: %d", resp.StatusCode)
		}

		b, err := ioutil.ReadAll(resp.Body)
		ri := &types.RuntimeInfo{}
		if err := json.Unmarshal(b, ri); err != nil {
			t.Fatalf("Unable to parse runtime image's response payload: %v", err)
		}

		if got, want := ri.Request.Headers.Get(headerName), name; got != want {
			t.Errorf("Headers[%q] = %q, wanted %q", headerName, got, want)
		}
	})

	t.Run("Check with passing header", func(t *testing.T) {
		t.Skip("https://github.com/knative/serving/issues/6303")

		req, err := http.NewRequest(http.MethodGet, "http://"+name+".example.com", nil)
		if err != nil {
			t.Fatalf("Error creating Request: %v", err)
		}

		// Specify a value for the header to verify that implementations
		// use set vs. append semantics.
		req.Header.Set(headerName, "bogus")

		// Make a request and check the response.
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Error making GET request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Got non-OK status: %d", resp.StatusCode)
		}

		b, err := ioutil.ReadAll(resp.Body)
		ri := &types.RuntimeInfo{}
		if err := json.Unmarshal(b, ri); err != nil {
			t.Fatalf("Unable to parse runtime image's response payload: %v", err)
		}

		if got, want := ri.Request.Headers.Get(headerName), name; got != want {
			t.Errorf("Headers[%q] = %q, wanted %q", headerName, got, want)
		}
	})
}

// TestPostSplitSetHeaders verifies that an Ingress that specified AppendHeaders post-split has the appropriate header(s) set.
func TestPostSplitSetHeaders(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	const headerName = "Foo-Bar-Baz"

	backends := make([]v1alpha1.IngressBackendSplit, 0, 10)
	names := sets.NewString()
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
			Percent: 10,
		})
		names.Insert(name)
	}

	// Create a simple Ingress over the 10 Services.
	name := test.ObjectNameForTest(t)
	_, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{name + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: backends,
				}},
			},
		}},
	})
	defer cancel()

	t.Run("Check without passing header", func(t *testing.T) {
		// Make enough requests that the likelihood of us seeing each variation is high,
		// but don't check the distribution of requests, as that isn't the point of this
		// particular test.
		seen := sets.NewString()
		for i := 0; i < 100; i++ {
			// Make a request and check the response.
			resp, err := client.Get("http://" + name + ".example.com")
			if err != nil {
				t.Fatalf("Error making GET request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Got non-OK status: %d", resp.StatusCode)
				continue
			}

			b, err := ioutil.ReadAll(resp.Body)
			ri := &types.RuntimeInfo{}
			if err := json.Unmarshal(b, ri); err != nil {
				t.Fatalf("Unable to parse runtime image's response payload: %v", err)
			}
			seen.Insert(ri.Request.Headers.Get(headerName))
		}
		// Check what we saw.
		if !cmp.Equal(names, seen) {
			t.Errorf("(over 100 requests) Header[%q] (-want, +got) = %s",
				headerName, cmp.Diff(names, seen))
		}
	})

	t.Run("Check with passing header", func(t *testing.T) {
		t.Skip("https://github.com/knative/serving/issues/6303")

		// Make enough requests that the likelihood of us seeing each variation is high,
		// but don't check the distribution of requests, as that isn't the point of this
		// particular test.
		seen := sets.NewString()
		for i := 0; i < 100; i++ {
			req, err := http.NewRequest(http.MethodGet, "http://"+name+".example.com", nil)
			if err != nil {
				t.Fatalf("Error creating Request: %v", err)
			}

			// Specify a value for the header to verify that implementations
			// use set vs. append semantics.
			req.Header.Set(headerName, "bogus")

			// Make a request and check the response.
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Error making GET request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Got non-OK status: %d", resp.StatusCode)
				continue
			}

			b, err := ioutil.ReadAll(resp.Body)
			ri := &types.RuntimeInfo{}
			if err := json.Unmarshal(b, ri); err != nil {
				t.Fatalf("Unable to parse runtime image's response payload: %v", err)
			}
			seen.Insert(ri.Request.Headers.Get(headerName))
		}
		// Check what we saw.
		if !cmp.Equal(names, seen) {
			t.Errorf("(over 100 requests) Header[%q] (-want, +got) = %s",
				headerName, cmp.Diff(names, seen))
		}
	})
}
