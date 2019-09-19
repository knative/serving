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
	svc, err := clients.ServingClient.Services.Create(service)
	return svc, err
}

func updateServiceWithTimeout(clients *test.Clients, names test.ResourceNames, revisionTimeoutSeconds int) error {
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
	if err != nil {
		return err
	}
	return nil
}

// sendRequests send a request to "endpoint", returns error if unexpected response code, nil otherwise.
func sendRequest(t *testing.T, clients *test.Clients, endpoint *url.URL, initialSleepSeconds int, sleepSeconds int, expectedResponseCode int) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, endpoint.Hostname(), test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Logf("Spoofing client failed: %v", err)
		return err
	}

	initialSleepMs := initialSleepSeconds * 1000
	sleepMs := sleepSeconds * 1000

	start := time.Now().UnixNano()
	defer func() {
		end := time.Now().UnixNano()
		t.Logf("URL: %v, initialSleep: %v, sleep: %v, request elapsed %.2f ms", endpoint, initialSleepMs, sleepMs, float64(end-start)/1e6)
	}()
	u, _ := url.Parse(endpoint.String())
	q := u.Query()
	q.Set("initialTimeout", fmt.Sprintf("%d", initialSleepMs))
	q.Set("timeout", fmt.Sprintf("%d", sleepMs))
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		t.Logf("Failed new request: %v", err)
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Logf("Failed request err: %v", err)
		return err
	}

	t.Logf("Response status code: %v, expected: %v", resp.StatusCode, expectedResponseCode)
	if expectedResponseCode != resp.StatusCode {
		return fmt.Errorf("got response status code %v, wanted %v", resp.StatusCode, expectedResponseCode)
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
		test.ServingFlags.ResolvableDomain); err != nil {
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
	if err := sendRequest(t, clients, rev2sURL, 5, 0, http.StatusServiceUnavailable); err != nil {
		t.Errorf("Did not fail request with sleep 5s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(t, clients, rev5sURL, 7, 0, http.StatusServiceUnavailable); err != nil {
		t.Errorf("Did not fail request with sleep 7s with revision timeout 5s: %v", err)
	}

	// Not fail by not surpassing in the initial timeout, but in the overall request duration.
	if err := sendRequest(t, clients, rev2sURL, 1, 3, http.StatusOK); err != nil {
		t.Errorf("Did not fail request with sleep 1s/3s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(t, clients, rev5sURL, 3, 3, http.StatusOK); err != nil {
		t.Errorf("Failed request with sleep 3s/3s with revision timeout 5s: %v", err)
	}
}
