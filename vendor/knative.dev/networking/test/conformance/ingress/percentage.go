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
	"errors"
	"math"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/pkg/pool"
)

// TestPercentage verifies that an Ingress splitting over multiple backends respects
// the given percentage distribution.
func TestPercentage(t *testing.T) {
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
			ri := RuntimeRequest(ctx, t, client, "http://"+name+".example.com")
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
