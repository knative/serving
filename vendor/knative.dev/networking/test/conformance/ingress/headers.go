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
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/ingress"
	"knative.dev/networking/test"
	"knative.dev/pkg/ptr"
)

// TestProbeHeaders verifies that an KIngress implemented the dataplane contract for probe request.
func TestProbeHeaders(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// Create a simple Ingress over the Service.
	ing, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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
	})

	bytes, err := ingress.ComputeHash(ing)
	if err != nil {
		t.Error("Failed to compute hash:", err)
	}

	tests := []struct {
		name string
		req  string
		want string
	}{{
		name: "kingress generates hash",
		req:  network.HashHeaderValue,
		want: fmt.Sprintf("%x", bytes),
	}, {
		name: "request overrides hash",
		req:  "2701a1b241db6af811992c57a5e11171847148ac3d2e1a8cc992a62f9e4fa111", // random hash to override.
		want: "2701a1b241db6af811992c57a5e11171847148ac3d2e1a8cc992a62f9e4fa111",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ros := []RequestOption{}

			ros = append(ros, func(r *http.Request) {
				// Add the header to indicate this is a probe request.
				r.Header.Set(network.ProbeHeaderName, network.ProbeHeaderValue)
				r.Header.Set(network.HashHeaderName, tt.req)
			})

			ri := RuntimeRequest(ctx, t, client, "http://"+name+".example.com", ros...)
			if ri == nil {
				t.Error("Couldn't make request")
				return
			}

			if got, want := ri.Request.Headers.Get(network.HashHeaderName), tt.want; got != want {
				t.Errorf("Header[%q] = %q, wanted %q", network.HashHeaderName, got, want)
			}
		})
	}

}

// TestTagHeaders verifies that an Ingress properly dispatches to backends based on the tag header
//
// See proposal doc for reference:
// https://docs.google.com/document/d/12t_3NE4EqvW_l0hfVlQcAGKkwkAM56tTn2wN_JtHbSQ/edit?usp=sharing
func TestTagHeaders(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	const (
		tagName           = "the-tag"
		backendHeader     = "Which-Backend"
		backendWithTag    = "tag"
		backendWithoutTag = "no-tag"
	)

	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{name + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Headers: map[string]v1alpha1.HeaderMatch{
						network.TagHeaderName: {
							Exact: tagName,
						},
					},
					AppendHeaders: map[string]string{
						backendHeader: backendWithTag,
					},
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      name,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(port),
						},
					}},
				}, {
					AppendHeaders: map[string]string{
						backendHeader: backendWithoutTag,
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

	tests := []struct {
		Name        string
		TagHeader   *string
		WantBackend string
	}{{
		Name:        "matching tag header",
		TagHeader:   ptr.String(tagName),
		WantBackend: backendWithTag,
	}, {
		Name:        "no tag header",
		WantBackend: backendWithoutTag,
	}, {
		// Note: Behavior may change in Phase 2 (see Proposal doc)
		Name:        "empty tag header",
		TagHeader:   ptr.String(""),
		WantBackend: backendWithoutTag,
	}, {
		// Note: Behavior may change in Phase 2 (see Proposal doc)
		Name:        "non-matching tag header",
		TagHeader:   ptr.String("not-" + tagName),
		WantBackend: backendWithoutTag,
	}}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			ros := []RequestOption{}

			if tt.TagHeader != nil {
				ros = append(ros, func(r *http.Request) {
					r.Header.Set(network.TagHeaderName, *tt.TagHeader)
				})
			}

			ri := RuntimeRequest(ctx, t, client, "http://"+name+".example.com", ros...)
			if ri == nil {
				t.Error("Couldn't make request")
				return
			}

			if got, want := ri.Request.Headers.Get(backendHeader), tt.WantBackend; got != want {
				t.Errorf("Header[%q] = %q, wanted %q", backendHeader, got, want)
			}
		})
	}

}

// TestPreSplitSetHeaders verifies that an Ingress that specified AppendHeaders pre-split has the appropriate header(s) set.
func TestPreSplitSetHeaders(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	const headerName = "Foo-Bar-Baz"

	// Create a simple Ingress over the 10 Services.
	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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

	t.Run("Check without passing header", func(t *testing.T) {
		ri := RuntimeRequest(ctx, t, client, "http://"+name+".example.com")
		if ri == nil {
			return
		}

		if got, want := ri.Request.Headers.Get(headerName), name; got != want {
			t.Errorf("Headers[%q] = %q, wanted %q", headerName, got, want)
		}
	})

	t.Run("Check with passing header", func(t *testing.T) {
		ri := RuntimeRequest(ctx, t, client, "http://"+name+".example.com", func(req *http.Request) {
			// Specify a value for the header to verify that implementations
			// use set vs. append semantics.
			req.Header.Set(headerName, "bogus")
		})
		if ri == nil {
			return
		}

		if got, want := ri.Request.Headers.Get(headerName), name; got != want {
			t.Errorf("Headers[%q] = %q, wanted %q", headerName, got, want)
		}
	})
}

// TestPostSplitSetHeaders verifies that an Ingress that specified AppendHeaders post-split has the appropriate header(s) set.
func TestPostSplitSetHeaders(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	const (
		headerName  = "Foo-Bar-Baz"
		splits      = 4
		maxRequests = 100
	)

	backends := make([]v1alpha1.IngressBackendSplit, 0, splits)
	names := make(sets.String, splits)
	for i := 0; i < splits; i++ {
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
			Percent: 100 / splits,
		})
		names.Insert(name)
	}

	// Create a simple Ingress over the 10 Services.
	name := test.ObjectNameForTest(t)
	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
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

	t.Run("Check without passing header", func(t *testing.T) {
		// Make enough requests that the likelihood of us seeing each variation is high,
		// but don't check the distribution of requests, as that isn't the point of this
		// particular test.
		seen := make(sets.String, len(names))
		for i := 0; i < maxRequests; i++ {
			ri := RuntimeRequest(ctx, t, client, "http://"+name+".example.com")
			if ri == nil {
				return
			}
			seen.Insert(ri.Request.Headers.Get(headerName))
			if seen.Equal(names) {
				// Short circuit if we've seen all headers.
				return
			}
		}
		// Us getting here means we haven't seen all headers, print the diff.
		t.Errorf("(over %d requests) Header[%q] (-want, +got) = %s",
			maxRequests, headerName, cmp.Diff(names, seen))
	})

	t.Run("Check with passing header", func(t *testing.T) {
		// Make enough requests that the likelihood of us seeing each variation is high,
		// but don't check the distribution of requests, as that isn't the point of this
		// particular test.
		seen := make(sets.String, len(names))
		for i := 0; i < maxRequests; i++ {
			ri := RuntimeRequest(ctx, t, client, "http://"+name+".example.com", func(req *http.Request) {
				// Specify a value for the header to verify that implementations
				// use set vs. append semantics.
				req.Header.Set(headerName, "bogus")
			})
			if ri == nil {
				return
			}
			seen.Insert(ri.Request.Headers.Get(headerName))
			if seen.Equal(names) {
				// Short circuit if we've seen all headers.
				return
			}
		}
		// Us getting here means we haven't seen all headers, print the diff.
		t.Errorf("(over %d requests) Header[%q] (-want, +got) = %s",
			maxRequests, headerName, cmp.Diff(names, seen))
	})
}
