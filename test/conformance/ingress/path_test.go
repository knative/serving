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

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/types"
)

// TestPath verifies that an Ingress properly dispatches to backends based on the path of the URL.
func TestPath(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	// For /foo
	fooName, fooPort, cancel := CreateService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	// For /bar
	barName, barPort, cancel := CreateService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	// For /baz
	bazName, bazPort, cancel := CreateService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	name, port, cancel := CreateService(t, clients, networking.ServicePortNameHTTP1)
	defer cancel()

	// Use a post-split injected header to establish which split we are sending traffic to.
	const headerName = "Which-Backend"

	_, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{name + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
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
							ServiceName:      name,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(port),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: name,
						},
						Percent: 100,
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
		"":      name,
		"/asdf": name,
	}

	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			// Make a request and check the response.
			resp, err := client.Get("http://" + name + ".example.com" + path)
			if err != nil {
				t.Fatalf("Error creating Ingress: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Got non-OK status: %d", resp.StatusCode)
			}

			b, err := ioutil.ReadAll(resp.Body)
			ri := &types.RuntimeInfo{}
			if err := json.Unmarshal(b, ri); err != nil {
				t.Fatalf("Unable to parse runtime image's response payload: %v", err)
			}

			got := ri.Request.Headers.Get(headerName)
			if got != want {
				t.Errorf("Header[%q] = %q, wanted %q", headerName, got, want)
			}
		})
	}
}
