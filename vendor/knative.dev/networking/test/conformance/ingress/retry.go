/*
Copyright 2021 The Knative Authors

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

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
)

// TestRetry verifies that the ingress does not retry failed requests.
func TestRetry(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)
	name, port, _ := CreateRetryService(ctx, t, clients)
	domain := name + ".example.com"

	// Create a simple Ingress over the Service.
	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{domain},
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

	// First try - we expect this to fail, because we shouldn't retry
	// automatically and the service only responds 200 on the _second_ access.
	resp, err := client.Get("http://" + domain)
	if err != nil {
		t.Errorf("Error making GET request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Got status %d, expected %d", resp.StatusCode, http.StatusServiceUnavailable)
		DumpResponse(ctx, t, resp)
	}

	// Second try - this time we should succeed.
	resp, err = client.Get("http://" + domain)
	if err != nil {
		t.Errorf("Error making GET request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Got not OK status, expected success: %d", resp.StatusCode)
		DumpResponse(ctx, t, resp)
	}
}
