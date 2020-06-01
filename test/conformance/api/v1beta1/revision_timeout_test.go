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

package v1beta1

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1b1test "knative.dev/serving/test/v1beta1"

	. "knative.dev/serving/pkg/testing/v1beta1"
)

// createService creates a service in namespace with the name names.Service
// that uses the image specified by names.Image
func createService(t *testing.T, clients *test.Clients, names test.ResourceNames, revisionTimeoutSeconds int64) (*v1beta1.Service, error) {
	service := v1b1test.Service(names, WithRevisionTimeoutSeconds(revisionTimeoutSeconds))
	v1b1test.LogResourceObject(t, v1b1test.ResourceObjects{Service: service})
	return clients.ServingBetaClient.Services.Create(service)
}

// sendRequests send a request to "endpoint", returns error if unexpected response code, nil otherwise.
func sendRequest(t *testing.T, clients *test.Clients, endpoint *url.URL,
	initialSleep, sleep time.Duration, expectedResponseCode int) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, endpoint.Hostname(), test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
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
	q.Set("initialTimeout", fmt.Sprintf("%d", initialSleep.Milliseconds()))
	q.Set("timeout", fmt.Sprintf("%d", sleep.Milliseconds()))
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
		return fmt.Errorf("response status code = %v, want: %v", resp.StatusCode, expectedResponseCode)
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
		expectedStatus int
		initialSleep   int
		sleep          int
	}{{
		name:           "when scaling up from 0 and does not exceed timeout seconds",
		shouldScaleTo0: true,
		timeoutSeconds: 10,
		expectedStatus: http.StatusOK,
		initialSleep:   0,
		sleep:          0,
	}, {
		name:           "when scaling up from 0 and it writes first byte before timeout",
		shouldScaleTo0: true,
		timeoutSeconds: 10,
		expectedStatus: http.StatusOK,
		initialSleep:   0,
		sleep:          15,
	}, {
		name:           "when scaling up from 0 and it does exceed timeout seconds",
		shouldScaleTo0: true,
		timeoutSeconds: 1, // If the pods come up faster than 1s, this test might fail.
		expectedStatus: http.StatusGatewayTimeout,
	}, {
		name:           "when pods already exist, and it does not exceed timeout seconds",
		timeoutSeconds: 10,
		expectedStatus: http.StatusOK,
		initialSleep:   2,
		sleep:          0,
	}, {
		name:           "when pods already exist, and it does exceed timeout seconds",
		timeoutSeconds: 10,
		expectedStatus: http.StatusGatewayTimeout,
		initialSleep:   12,
		sleep:          0,
	}, {
		name:           "when pods already exist, and it writes first byte before timeout",
		timeoutSeconds: 10,
		expectedStatus: http.StatusOK,
		initialSleep:   0,
		sleep:          15,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.Timeout,
			}

			test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
			defer test.TearDown(clients, names)

			t.Log("Creating a new Service ")
			_, err := createService(t, clients, names, tc.timeoutSeconds)
			if err != nil {
				t.Fatal("Failed to create Service:", err)
			}

			t.Log("The Service will be updated with the name of the Revision once it is created")
			revisionName, err := v1b1test.WaitForServiceLatestRevision(clients, names)
			if err != nil {
				t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
			}
			names.Revision = revisionName

			t.Log("When the Service reports as Ready, everything should be ready")
			if err := v1b1test.WaitForServiceState(clients.ServingBetaClient, names.Service, v1b1test.IsServiceReady, "ServiceIsReady"); err != nil {
				t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
			}

			service, err := clients.ServingClient.Services.Get(names.Service, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Error fetching Service %s: %v", names.Service, err)
			}

			if service.Status.URL == nil {
				t.Fatalf("Unable to fetch URLs from service: %#v", service.Status)
			}

			serviceURL := url.URL(*service.Status.URL)

			if tc.shouldScaleTo0 {
				t.Log("Waiting to scale down to 0")
				revision, err := clients.ServingBetaClient.Revisions.Get(names.Revision, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Error fetching Service %s: %v", names.Service, err)
				}

				if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(revision), clients); err != nil {
					t.Fatal("Could not scale to zero:", err)
				}
			} else {
				t.Log("Probing to force at least pods", &serviceURL)
				if _, err := pkgTest.WaitForEndpointState(
					clients.KubeClient,
					t.Logf,
					&serviceURL,
					v1b1test.RetryingRouteInconsistency(pkgTest.IsOneOfStatusCodes(http.StatusOK, http.StatusGatewayTimeout)),
					"WaitForSuccessfulResponse",
					test.ServingFlags.ResolvableDomain,
					test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https)); err != nil {
					t.Fatalf("Error probing %s: %v", &serviceURL, err)
				}
			}

			if err := sendRequest(t, clients, &serviceURL, time.Duration(tc.initialSleep)*time.Second, time.Duration(tc.sleep)*time.Second, tc.expectedStatus); err != nil {
				t.Errorf("Failed request with intialSleep %ds, sleep %ds, with revision timeout %ds and expecting status %v: %v",
					tc.initialSleep, tc.sleep, tc.timeoutSeconds, tc.expectedStatus, err)
			}
		})
	}
}
