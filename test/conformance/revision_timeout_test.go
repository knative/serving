// +build e2e

/*
Copyright 2018 The Knative Authors

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

package conformance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"github.com/mattbaird/jsonpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// createLatestService creates a service in namespace with the name names.Service
// that uses the image specified by imagePath
func createLatestService(logger *logging.BaseLogger, clients *test.Clients, names test.ResourceNames, imagePath string, revisionTimeoutSeconds int64) (*v1alpha1.Service, error) {
	service := test.LatestService(test.ServingNamespace, names, imagePath, &test.Options{})
	service.Spec.RunLatest.Configuration.RevisionTemplate.Spec.TimeoutSeconds = revisionTimeoutSeconds
	test.LogResourceObject(logger, test.ResourceObjects{Service: service})
	svc, err := clients.ServingClient.Services.Create(service)
	return svc, err
}

func updateConfigWithTimeout(clients *test.Clients, names test.ResourceNames, revisionTimeoutSeconds int) error {
	patches := []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/revisionTemplate/spec/timeoutSeconds",
		Value:     revisionTimeoutSeconds,
	}}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	_, err = clients.ServingClient.Configs.Patch(names.Config, types.JSONPatchType, patchBytes, "")
	if err != nil {
		return err
	}
	return nil
}

// sendRequests send a request to "domain", returns error if unexpected response code, nil otherwise.
func sendRequest(logger *logging.BaseLogger, clients *test.Clients, domain string, initialSleepSeconds int, sleepSeconds int, expectedResponseCode int) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		logger.Infof("Spoofing client failed: %v", err)
		return err
	}

	initialSleepMs := initialSleepSeconds * 1000
	sleepMs := sleepSeconds * 1000

	start := time.Now().UnixNano()
	defer func() {
		end := time.Now().UnixNano()
		logger.Infof("domain: %v, initialSleep: %v, sleep: %v, request elapsed %.2f ms", domain, initialSleepMs, sleepMs, float64(end-start)/1e6)
	}()
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s?initialTimeout=%v&timeout=%v", domain, initialSleepMs, sleepMs), nil)
	if err != nil {
		logger.Infof("Failed new request: %v", err)
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Infof("Failed request err: %v", err)
		return err
	}

	logger.Infof("Response status code: %v, expected: %v", resp.StatusCode, expectedResponseCode)
	if expectedResponseCode != resp.StatusCode {
		return fmt.Errorf("got response status code %v, wanted %v", resp.StatusCode, expectedResponseCode)
	}
	return nil
}

func TestRevisionTimeout(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestRevisionTimeout")

	imagePath := test.ImagePath(timeout)

	var names, rev2s, rev5s test.ResourceNames
	names.Service = test.AppendRandomString("timeout", logger)

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	logger.Info("Creating a new Service in runLatest")
	svc, err := createLatestService(logger, clients, names, imagePath, 2)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	logger.Info("The Service will be updated with the name of the Revision once it is created")
	revisionName, err := test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	rev2s.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}

	logger.Info("Patching to a Manual Service to allow configuration and route to be manually modified")
	_, err = test.PatchManualService(logger, clients, svc)
	if err != nil {
		t.Fatalf("Failed to update Service %s: %v", names.Service, err)
	}

	logger.Infof("Updating the Configuration to use a different revision timeout")
	err = updateConfigWithTimeout(clients, names, 5)
	if err != nil {
		t.Fatalf("Patch update for Configuration %s with new timeout 5s failed: %v", names.Config, err)
	}

	// getNextRevisionName waits for names.Revision to change, so we set it to the rev2s revision and wait for the (new) rev5s revision.
	names.Revision = rev2s.Revision

	logger.Infof("Since the Configuration was updated a new Revision will be created and the Configuration will be updated")
	rev5s.Revision, err = test.WaitForConfigLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the Revision with timeout 5s: %v", names.Config, err)
	}

	logger.Infof("Waiting for revision %q to be ready", rev2s.Revision)
	if err := test.WaitForRevisionState(clients.ServingClient, rev2s.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q still can't serve traffic: %v", rev2s.Revision, err)
	}
	logger.Infof("Waiting for revision %q to be ready", rev5s.Revision)
	if err := test.WaitForRevisionState(clients.ServingClient, rev5s.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q still can't serve traffic: %v", rev5s.Revision, err)
	}

	// Set names for traffic targets to make them directly routable.
	rev2s.TrafficTarget = "rev2s"
	rev5s.TrafficTarget = "rev5s"

	logger.Infof("Updating Route")
	if _, err := test.UpdateBlueGreenRoute(logger, clients, names, rev2s, rev5s); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	logger.Infof("Wait for the route domains to be ready")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}

	rev2sDomain := fmt.Sprintf("%s.%s", rev2s.TrafficTarget, route.Status.Domain)
	rev5sDomain := fmt.Sprintf("%s.%s", rev5s.TrafficTarget, route.Status.Domain)

	logger.Infof("Probing domain %s", rev5sDomain)
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		rev5sDomain,
		pkgTest.Retrying(pkgTest.MatchesAny, http.StatusNotFound, http.StatusServiceUnavailable),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing domain %s: %v", rev5sDomain, err)
	}

	// Quick sanity check
	if err := sendRequest(logger, clients, rev2sDomain, 0, 0, http.StatusOK); err != nil {
		t.Errorf("Failed request with sleep 0s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(logger, clients, rev5sDomain, 0, 0, http.StatusOK); err != nil {
		t.Errorf("Failed request with sleep 0s with revision timeout 5s: %v", err)
	}

	// Fail by surpassing the initial timeout.
	if err := sendRequest(logger, clients, rev2sDomain, 5, 0, http.StatusServiceUnavailable); err != nil {
		t.Errorf("Did not fail request with sleep 5s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(logger, clients, rev5sDomain, 7, 0, http.StatusServiceUnavailable); err != nil {
		t.Errorf("Did not fail request with sleep 7s with revision timeout 5s: %v", err)
	}

	// Not fail by not surpassing in the initial timeout, but in the overall request duration.
	if err := sendRequest(logger, clients, rev2sDomain, 1, 3, http.StatusOK); err != nil {
		t.Errorf("Did not fail request with sleep 1s/3s with revision timeout 2s: %v", err)
	}
	if err := sendRequest(logger, clients, rev5sDomain, 3, 3, http.StatusOK); err != nil {
		t.Errorf("Failed request with sleep 3s/3s with revision timeout 5s: %v", err)
	}
}
