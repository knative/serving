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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/test"
)

// TestTimeout verifies that an Ingress configured with a timeout respects that.
func TestTimeout(t *testing.T) {
	t.Parallel()
	defer logstream.Start(t)()
	clients := test.Setup(t)

	name, port, cancel := CreateTimeoutService(t, clients)
	defer cancel()

	// The timeout, and an epsilon value to use as jitter for testing requests
	// either hit or miss the timeout (without getting so close that we flake).
	const (
		timeout = 1 * time.Second
		epsilon = 100 * time.Millisecond
	)

	// Create a simple Ingress over the Service.
	_, client, cancel := CreateIngressReady(t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{name + ".example.com"},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Timeout: &metav1.Duration{Duration: timeout},
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

	tests := []struct {
		name         string
		code         int
		initialDelay time.Duration
		delay        time.Duration
	}{{
		name: "no delays is OK",
		code: http.StatusOK,
	}, {
		name:  "large delay after headers is ok",
		code:  http.StatusOK,
		delay: timeout + timeout,
	}, {
		name:         "initial delay less than timeout is ok",
		code:         http.StatusOK,
		initialDelay: timeout - epsilon,
	}, {
		name:         "initial delay over timeout is NOT ok",
		code:         http.StatusGatewayTimeout,
		initialDelay: timeout + epsilon,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkTimeout(t, client, name, test.code, test.initialDelay, test.delay)
		})
	}
}

func checkTimeout(t *testing.T, client *http.Client, name string, code int, initial time.Duration, timeout time.Duration) {
	t.Helper()

	resp, err := client.Get(fmt.Sprintf("http://%s.example.com?initialTimeout=%d&timeout=%d",
		name, initial.Milliseconds(), timeout.Milliseconds()))
	if err != nil {
		t.Fatal("Error making GET request:", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != code {
		t.Errorf("Unexpected status code: %d, wanted %d", resp.StatusCode, code)
		DumpResponse(t, resp)
	}
}
