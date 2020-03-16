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
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/mattbaird/jsonpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	. "knative.dev/serving/pkg/testing/v1"
)

// createService creates a service in namespace with the name names.Service
// that uses the image specified by names.Image
func createService(t *testing.T, clients *test.Clients, names test.ResourceNames, revisionTimeoutSeconds int64) (*v1.Service, error) {
	service := v1test.Service(names, WithRevisionTimeoutSeconds(revisionTimeoutSeconds))
	v1test.LogResourceObject(t, v1test.ResourceObjects{Service: service})
	return clients.ServingClient.Services.Create(service)
}

func updateServiceWithTimeout(clients *test.Clients, names test.ResourceNames, revisionTimeoutSeconds int64) error {
	patches := []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/template/spec/timeoutSeconds",
		Value:     revisionTimeoutSeconds,
	}}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	_, err = clients.ServingClient.Services.Patch(names.Service, types.JSONPatchType, patchBytes, "")
	return err
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

	var rev2s, rev5s test.ResourceNames
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Timeout,
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service ")
	svc, err := createService(t, clients, names, 2)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	t.Log("The Service will be updated with the name of the Revision once it is created")
	revisionName, err := v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	rev2s.Revision = revisionName

	t.Log("When the Service reports as Ready, everything should be ready")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}

	t.Log("Updating the Service to use a different revision timeout")
	err = updateServiceWithTimeout(clients, names, 5)
	if err != nil {
		t.Fatalf("Patch update for Service %s with new timeout 5s failed: %v", names.Service, err)
	}

	// getNextRevisionName waits for names.Revision to change, so we set it to the rev2s revision and wait for the (new) rev5s revision.
	names.Revision = rev2s.Revision

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	rev5s.Revision, err = v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision with timeout 5s: %v", names.Service, err)
	}

	t.Logf("Waiting for revision %q to be ready", rev2s.Revision)
	if err := v1test.WaitForRevisionState(clients.ServingClient, rev2s.Revision, v1test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q still can't serve traffic: %v", rev2s.Revision, err)
	}
	t.Logf("Waiting for revision %q to be ready", rev5s.Revision)
	if err := v1test.WaitForRevisionState(clients.ServingClient, rev5s.Revision, v1test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q still can't serve traffic: %v", rev5s.Revision, err)
	}

	// Set names for traffic targets to make them directly routable.
	rev2s.TrafficTarget = "rev2s"
	rev5s.TrafficTarget = "rev5s"

	t.Log("Updating RouteSpec")
	if _, err := v1test.UpdateServiceRouteSpec(t, clients, names, v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			Tag:          rev2s.TrafficTarget,
			RevisionName: rev2s.Revision,
			Percent:      ptr.Int64(50),
		}, {
			Tag:          rev5s.TrafficTarget,
			RevisionName: rev5s.Revision,
			Percent:      ptr.Int64(50),
		}},
	}); err != nil {
		t.Fatalf("Failed to update Service: %v", err)
	}

	t.Log("Wait for the service domains to be ready")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	service, err := clients.ServingClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Service %s: %v", names.Service, err)
	}

	var rev2sURL, rev5sURL *url.URL
	for _, tt := range service.Status.Traffic {
		if tt.Tag == rev2s.TrafficTarget {
			rev2sURL = tt.URL.URL()
		}
		if tt.Tag == rev5s.TrafficTarget {
			rev5sURL = tt.URL.URL()
		}
	}
	if rev2sURL == nil || rev5sURL == nil {
		t.Fatalf("Unable to fetch URLs from traffic targets: %#v", service.Status.Traffic)
	}

	t.Logf("Probing %s", rev5sURL)
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		rev5sURL,
		v1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https)); err != nil {
		t.Fatalf("Error probing %s: %v", rev5sURL, err)
	}

	// Quick sanity check
	if err := sendRequest(t, clients, rev2sURL, 0, 0, http.StatusOK); err != nil {
		t.Errorf("Failed request with sleep 0s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(t, clients, rev5sURL, 0, 0, http.StatusOK); err != nil {
		t.Errorf("Failed request with sleep 0s with revision timeout 5s: %v", err)
	}

	// Fail by surpassing the initial timeout.
	if err := sendRequest(t, clients, rev2sURL, 5*time.Second, 0, http.StatusGatewayTimeout); err != nil {
		t.Errorf("Did not fail request with sleep 5s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(t, clients, rev5sURL, 7*time.Second, 0, http.StatusGatewayTimeout); err != nil {
		t.Errorf("Did not fail request with sleep 7s with revision timeout 5s: %v", err)
	}

	// Not fail by not surpassing in the initial timeout, but in the overall request duration.
	if err := sendRequest(t, clients, rev2sURL, time.Second, 3*time.Second, http.StatusOK); err != nil {
		t.Errorf("Did not fail request with sleep 1s/3s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(t, clients, rev5sURL, 3*time.Second, 3*time.Second, http.StatusOK); err != nil {
		t.Errorf("Failed request with sleep 3s/3s with revision timeout 5s: %v", err)
	}
}
