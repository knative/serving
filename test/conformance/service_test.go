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
	"strings"
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresources "github.com/knative/serving/pkg/controller/service/resources"
	"github.com/knative/serving/test"
	"github.com/mattbaird/jsonpatch"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
)

func tearDownService(clients *test.Clients, names test.ResourceNames) {
	if clients != nil {
		if clients.Services != nil {
			clients.Services.Delete(names.Service, nil)
		}
	}
}

func updateServiceWithImage(clients *test.Clients, names test.ResourceNames, imagePath string) error {
	patches := []jsonpatch.JsonPatchOperation{
		jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/runLatest/configuration/revisionTemplate/spec/container/image",
			Value:     imagePath,
		},
	}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	newService, err := clients.Services.Patch(names.Service, types.JSONPatchType, patchBytes, "")
	if err != nil {
		return err
	}
	if newService.Spec.Generation != int64(2) {
		return fmt.Errorf("The spec was updated so the Generation should be 2 but it was actually %d", newService.Spec.Generation)
	}
	return nil
}

// Shamelessly cribbed from route_test. We expect the Route and Configuration to be ready if the Service is ready.
func assertServiceResourcesUpdated(t *testing.T, logger *zap.SugaredLogger, clients *test.Clients, names test.ResourceNames, routeDomain, expectedText string) {
	expectedEndpointState := test.NewExpectedEndpointstate(routeDomain)
	// TODO(#1178): Remove "Wait" from all checks below this point.
	err := test.WaitForEndpointState(clients.Kube, logger, test.Flags.ResolvableDomain, expectedEndpointState, test.EventuallyMatchesBody(expectedText), "WaitForEndpointToServeText")
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, routeDomain, expectedText, err)
	}

	// We want to verify that the endpoint works as soon as Ready: True, but there are a bunch of other pieces of state that we validate for conformance.
	logger.Info("The Revision will be marked as Ready when it can serve traffic")
	if err := test.CheckRevisionState(clients.Revisions, names.Revision, test.IsRevisionReady); err != nil {
		t.Fatalf("Revision %s did not become ready to serve traffic: %v", names.Revision, err)
	}

	logger.Info("The Service's latestReadyRevisionName should match the Configuration's")
	err = test.CheckConfigurationState(clients.Configs, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		return c.Status.LatestReadyRevisionName == names.Revision, nil
	})
	if err != nil {
		t.Fatalf("The Configuration %s was not updated indicating that the Revision %s was ready: %v\n", names.Config, names.Revision, err)
	}

	logger.Info("Updates the Route to route traffic to the Revision")
	if err := test.CheckRouteState(clients.Routes, names.Route, test.AllRouteTrafficAtRevision(names)); err != nil {
		t.Fatalf("The Route %s was not updated to route traffic to the Revision %s: %v", names.Route, names.Revision, err)
	}

	// TODO(#1381): Check labels and annotations.
}

func waitForServiceLatestCreatedRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	var revisionName string
	err := test.WaitForServiceState(clients.Services, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	return revisionName, err
}

func waitForServiceDomain(clients *test.Clients, names test.ResourceNames) (string, error) {
	var routeDomain string
	err := test.WaitForServiceState(clients.Services, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.Domain != "" {
			routeDomain = s.Status.Domain
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithDomain")
	return routeDomain, err
}

func TestRunLatestService(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := test.Logger.Named("TestRunLatestService")

	var imagePaths []string
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image1}, "/"))
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image2}, "/"))

	var names test.ResourceNames
	names.Service = test.AppendRandomString("pizzaplanet-service", logger)

	defer tearDownService(clients, names)
	test.CleanupOnInterrupt(func() { tearDownService(clients, names) }, logger)

	logger.Info("Creating a new Service")
	svc, err := clients.Services.Create(test.LatestService(test.Flags.Namespace, names, imagePaths[0]))
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresources.RouteName(svc)
	names.Config = serviceresources.ConfigurationName(svc)

	logger.Info("The Service will be updated with the name of the Revision once it is created")
	revisionName, err := waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	names.Revision = revisionName

	logger.Info("The Service will be updated with the domain of the Route once it is created")
	routeDomain, err := waitForServiceDomain(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new route: %v", names.Service, err)
	}

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.Services, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "What a spaceport!")

	logger.Info("Updating the Service to use a different image")
	if err := updateServiceWithImage(clients, names, imagePaths[1]); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, imagePaths[1], err)
	}

	logger.Info("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, image2, err)
	}
	names.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.Services, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "Re-energize yourself with a slice of pepperoni!")
}

// TODO(jonjohnsonjr): LatestService roads less traveled.
// TODO(jonjohnsonjr): PinnedService happy path.
// TODO(jonjohnsonjr): PinnedService roads less traveled.
// TODO(jonjohnsonjr): Examples of deploying from source.
