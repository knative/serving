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
	"fmt"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
)

// TestRetry verifies that an Ingress configured to retry N times properly masks transient failures.
func TestRetry(t *testing.T) {
	for i := 2; i < 12; i++ {
		i := i
		t.Run(fmt.Sprintf("period=%d,attempts=%d", i, i), func(t *testing.T) {
			t.Parallel()
			clients := test.Setup(t)
			// When the period matches, then it shouldn't fail.
			name, port, cancel := CreateFlakyService(t, clients, i)
			defer cancel()

			domain := name + ".example.com"

			// Create a simple Ingress over the Service.
			_, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts:      []string{domain},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Retries: retries(i),
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

			for j := 0; j < 5; j++ {
				resp, err := client.Get("http://" + domain)
				if err != nil {
					t.Errorf("Error making GET request: %v", err)
					continue
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("Got non-OK status: %d", resp.StatusCode)
					DumpResponse(t, resp)
				}
			}
		})

		t.Run(fmt.Sprintf("period=%d,attempts=%d", i, i-1), func(t *testing.T) {
			t.Parallel()
			clients := test.Setup(t)
			// When the period matches, then it shouldn't fail.
			name, port, cancel := CreateFlakyService(t, clients, i)
			defer cancel()

			domain := name + ".example.com"

			// Create a simple Ingress over the Service.
			_, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts:      []string{domain},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Retries: retries(i - 1),
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

			// When the period is one more than the number of attempts (retries+1), then what we should see is:
			//   500 {count=4}
			//   200 {count=5}
			//   500 {count=9}
			//   200 {count=10}
			//   500 {count=14}
			//   200 {count=15}
			current, next := http.StatusInternalServerError, http.StatusOK
			for j := 0; j < 5; j++ {
				resp, err := client.Get("http://" + domain)
				if err != nil {
					t.Errorf("Error making GET request: %v", err)
					continue
				}
				defer resp.Body.Close()
				if want, got := current, resp.StatusCode; got != want {
					t.Errorf("Status = %d, wanted %d", got, want)
					DumpResponse(t, resp)
				}
				// Swap things.
				current, next = next, current
			}
		})
	}
}

func retries(attempts int) *v1alpha1.HTTPRetry {
	return &v1alpha1.HTTPRetry{
		// retries is one less than the number of attempts
		Attempts: attempts - 1,
	}
}
