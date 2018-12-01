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

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnvVars(t *testing.T) {
	clients := Setup(t)

	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestEnvVars")

	var imagePath = test.ImagePath("envvars")

	var names test.ResourceNames
	names.Service = test.AppendRandomString("yashiki", logger)

	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	var revisionName string
	logger.Info("The Service will be updated with the name of the Revision once it is created")
	err = test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	names.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}

	domain := route.Status.Domain + "/envvars/should"

	resp, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(verifyUpStatus, http.StatusNotFound),
		"EnvVarsServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Failed before reaching desired state : %v", err)
	}

	expectedOutputs := map[string]string{
		"K_SERVICE" : names.Service,
		"K_CONFIGURATION" : names.Config,
		"K_REVISION" : names.Revision,
	}
	// We are failing even for "SHOULD".
	if err = verifyResponse(resp, "SHOULD", expectedOutputs); err != nil {
		t.Error(err)
	}

	domain = route.Status.Domain + "/envvars/must"

	resp, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(verifyUpStatus, http.StatusNotFound),
		"EnvVarsServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Failed before reaching desired state : %v", err)
	}

	expectedOutputs = map[string]string{
		"PORT" : "8080",
	}
	if err = 	verifyResponse(resp, "MUST", expectedOutputs); err != nil {
		t.Error(err)
	}
}

func verifyUpStatus(resp *spoof.Response) (done bool, err error) {
	if resp.StatusCode ==  http.StatusOK {
		return true, nil
	}

	return true, errors.New(string(resp.Body))
}

func verifyResponse(resp *spoof.Response, constraint string, expectedOutputs map[string]string) error {
	var respMap map[string]string
	if jsonErr := json.Unmarshal(resp.Body, &respMap); jsonErr != nil {
		return errors.Errorf("Json Un-marshalling error : %v", jsonErr)
	}

	if len(expectedOutputs) != len(respMap) {
		return errors.Errorf("Expected number of %s environment variables don't match. Expected : %d, Got : %d", constraint, len(expectedOutputs), len(respMap))
	}

	for key, value := range expectedOutputs {
		if v, ok := respMap[key]; ok {
			if value != v {
				return errors.Errorf("%s environment variable : %s didn't match. Expected : %s Got : %s", constraint, key, value, v)
			}
		} else {
			return errors.Errorf("%s environment variable : %s not found", constraint, key)
		}
	}

	return nil
}