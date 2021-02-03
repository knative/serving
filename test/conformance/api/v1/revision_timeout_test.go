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

package v1

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"

	. "knative.dev/serving/pkg/testing/v1"
)

// sendRequests send a request to "endpoint", returns error if unexpected response code, nil otherwise.
func sendRequest(t *testing.T, clients *test.Clients, endpoint *url.URL,
	initialSleep, sleep time.Duration, expectedResponseCode int) error {
	client, err := pkgtest.NewSpoofingClient(context.Background(), clients.KubeClient, t.Logf, endpoint.Hostname(), test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		return fmt.Errorf("error creating Spoofing client: %w", err)
	}

	start := time.Now()
	defer func() {
		t.Logf("URL: %v, initialSleep: %v, sleep: %v, request elapsed %v ms", endpoint, initialSleep, sleep,
			time.Since(start).Milliseconds())
	}()
	u, _ := url.Parse(endpoint.String())
	q := u.Query()
	q.Set("initialTimeout", fmt.Sprint(initialSleep.Milliseconds()))
	q.Set("timeout", fmt.Sprint(sleep.Milliseconds()))
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create new HTTP request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed roundtripping: %w", err)
	}

	t.Logf("Response status code: %v, expected: %v", resp.StatusCode, expectedResponseCode)
	if expectedResponseCode != resp.StatusCode {
		return fmt.Errorf("response status code = %v, want = %v, response = %v", resp.StatusCode, expectedResponseCode, resp)
	}
	return nil
}

func TestRevisionTimeout(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	testCases := []struct {
		name           string
		shouldScaleTo0 bool
		timeoutSeconds int64
		initialSleep   time.Duration
		sleep          time.Duration
		expectedStatus int
	}{{
		name:           "when pods already exist, and it does not exceed timeout seconds",
		timeoutSeconds: 10,
		initialSleep:   2 * time.Second,
		expectedStatus: http.StatusOK,
	}, {
		name:           "when pods already exist, and it does exceed timeout seconds",
		timeoutSeconds: 10,
		initialSleep:   12 * time.Second,
		expectedStatus: http.StatusGatewayTimeout,
	}, {
		name:           "when pods already exist, and it writes first byte before timeout",
		timeoutSeconds: 10,
		expectedStatus: http.StatusOK,
		sleep:          15 * time.Second,
		initialSleep:   0,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.Timeout,
			}

			test.EnsureTearDown(t, clients, &names)

			t.Log("Creating a new Service ")
			resources, err := v1test.CreateServiceReady(t, clients, &names, WithRevisionTimeoutSeconds(tc.timeoutSeconds))
			if err != nil {
				t.Fatal("Failed to create Service:", err)
			}

			serviceURL := resources.Service.Status.URL.URL()

			if tc.shouldScaleTo0 {
				t.Log("Waiting to scale down to 0")
				if err := shared.WaitForScaleToZero(t, resourcenames.Deployment(resources.Revision), clients); err != nil {
					t.Fatal("Could not scale to zero:", err)
				}
			} else {
				t.Log("Probing to force at least one pod", serviceURL)
				if _, err := pkgtest.WaitForEndpointState(
					context.Background(),
					clients.KubeClient,
					t.Logf,
					serviceURL,
					v1test.RetryingRouteInconsistency(spoof.IsOneOfStatusCodes(http.StatusOK, http.StatusGatewayTimeout)),
					"WaitForSuccessfulResponse",
					test.ServingFlags.ResolvableDomain,
					test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS)); err != nil {
					t.Fatalf("Error probing %s: %v", serviceURL, err)
				}
			}

			if err := sendRequest(t, clients, serviceURL, tc.initialSleep, tc.sleep, tc.expectedStatus); err != nil {
				t.Errorf("Failed request with initialSleep %v, sleep %v, with revision timeout %ds and expecting status %v: %v",
					tc.initialSleep, tc.sleep, tc.timeoutSeconds, tc.expectedStatus, err)
			}
		})
	}
}
