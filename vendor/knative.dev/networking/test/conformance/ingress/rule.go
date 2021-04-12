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
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
)

// TestRule verifies that an Ingress properly dispatches to backends based on different rules.
func TestRule(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	// Use a pre-split injected header to establish which rule we are sending traffic to.
	const headerName = "Foo-Bar-Baz"

	fooName, fooPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)
	barName, barPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{fooName + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      fooName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(fooPort),
						},
					}},
					// Append different headers to each rule, which lets us identify
					// which backend we hit.
					AppendHeaders: map[string]string{
						headerName: fooName,
					},
				}},
			},
		}, {
			Hosts:      []string{barName + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      barName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(barPort),
						},
					}},
					AppendHeaders: map[string]string{
						headerName: barName,
					},
				}},
			},
		}},
	})

	ri := RuntimeRequest(ctx, t, client, "http://"+fooName+".example.com")
	if got := ri.Request.Headers.Get(headerName); got != fooName {
		t.Errorf("Header[Host] = %q, wanted %q", got, fooName)
	}

	ri = RuntimeRequest(ctx, t, client, "http://"+barName+".example.com")
	if got := ri.Request.Headers.Get(headerName); got != barName {
		t.Errorf("Header[Host] = %q, wanted %q", got, barName)
	}
}
