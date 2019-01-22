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
	"fmt"
	"net/http"
	"strconv"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const userPort = int32(8081)

// Validates the state of Configuration, Revision, and Route objects for a runLatest Service. The checks in this method should be able to be performed at any point in a
// runLatest Service's lifecycle so long as the service is in a "Ready" state.
func validateRunLatestControlPlane(logger *logging.BaseLogger, clients *test.Clients, names test.ResourceNames, expectedGeneration string) error {
	logger.Info("Checking to ensure Revision is in desired state with generation: ", expectedGeneration)
	err := test.CheckRevisionState(clients.ServingClient, names.Revision, func(r *v1alpha1.Revision) (bool, error) {
		if ready, err := test.IsRevisionReady(r); !ready {
			return false, fmt.Errorf("revision %s did not become ready to serve traffic: %v", names.Revision, err)
		}
		if r.Status.ImageDigest == "" {
			return false, fmt.Errorf("imageDigest not present for revision %s", names.Revision)
		}
		if validDigest, err := validateImageDigest(names.Image, r.Status.ImageDigest); !validDigest {
			return false, fmt.Errorf("imageDigest %s is not valid for imageName %s: %v", r.Status.ImageDigest, names.Image, err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	err = test.CheckRevisionState(clients.ServingClient, names.Revision, test.IsRevisionAtExpectedGeneration(expectedGeneration))
	if err != nil {
		return fmt.Errorf("revision %s did not have an expected annotation with generation %s: %v", names.Revision, expectedGeneration, err)
	}

	logger.Info("Checking to ensure Configuration is in desired state.")
	err = test.CheckConfigurationState(clients.ServingClient, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
			return false, fmt.Errorf("the Configuration %s was not updated indicating that the Revision %s was created: %v", names.Config, names.Revision, err)
		}
		if c.Status.LatestReadyRevisionName != names.Revision {
			return false, fmt.Errorf("the Configuration %s was not updated indicating that the Revision %s was ready: %v", names.Config, names.Revision, err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	logger.Info("Checking to ensure Route is in desired state with generation: ", expectedGeneration)
	err = test.CheckRouteState(clients.ServingClient, names.Route, test.AllRouteTrafficAtRevision(names))
	if err != nil {
		return fmt.Errorf("the Route %s was not updated to route traffic to the Revision %s: %v", names.Route, names.Revision, err)
	}

	return nil
}

// Validates service health and vended content match for a runLatest Service. The checks in this method should be able to be performed at any point in a
// runLatest Service's lifecycle so long as the service is in a "Ready" state.
func validateRunLatestDataPlane(logger *logging.BaseLogger, clients *test.Clients, names test.ResourceNames, expectedText string) error {
	logger.Infof("Checking that the endpoint vends the expected text: %s", expectedText)
	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		names.Domain,
		pkgTest.Retrying(pkgTest.EventuallyMatchesBody(expectedText), http.StatusNotFound),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		return fmt.Errorf("the endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, names.Domain, expectedText, err)
	}

	logger.Infof("TODO: The Service's Route is accessible from inside the cluster without external DNS")
	err = test.CheckServiceState(clients.ServingClient, names.Service, test.TODO_ServiceTrafficToRevisionWithInClusterDNS)
	if err != nil {
		return fmt.Errorf("the Service %s was not able to route traffic to the Revision %s with in cluster DNS: %v", names.Service, names.Revision, err)
	}

	return nil

}

// Validates labels on Revision, Configuration, and Route objects when created by a Service
// see spec here: https://github.com/knative/serving/blob/master/docs/spec/spec.md#revision
func validateLabelsPropagation(logger *logging.BaseLogger, objects test.ResourceObjects, names test.ResourceNames) error {
	logger.Info("Validate Labels on Revision Object")
	revision := objects.Revision

	if revision.Labels["serving.knative.dev/configuration"] != names.Config {
		return fmt.Errorf("expect Confguration name in Revision label %q but got %q ", names.Config, revision.Labels["serving.knative.dev/configuration"])
	}
	if revision.Labels["serving.knative.dev/service"] != names.Service {
		return fmt.Errorf("expect Service name in Revision label %q but got %q ", names.Service, revision.Labels["serving.knative.dev/service"])
	}

	logger.Info("Validate Labels on Configuration Object")
	config := objects.Config
	if config.Labels["serving.knative.dev/service"] != names.Service {
		return fmt.Errorf("expect Service name in Configuration label %q but got %q ", names.Service, config.Labels["serving.knative.dev/service"])
	}
	if config.Labels["serving.knative.dev/route"] != names.Route {
		return fmt.Errorf("expect Route name in Configuration label %q but got %q ", names.Route, config.Labels["serving.knative.dev/route"])
	}

	logger.Info("Validate Labels on Route Object")
	route := objects.Route
	if route.Labels["serving.knative.dev/service"] != names.Service {
		return fmt.Errorf("expect Service name in Route label %q but got %q ", names.Service, route.Labels["serving.knative.dev/service"])
	}
	return nil
}

// TestRunLatestService tests both Creation and Update paths of a runLatest service. The test performs a series of Update/Validate steps to ensure that
// the service transitions as expected during each step.
// Currently the test performs the following updates:
// 1. Update Container Image
// 2. Update Metadata
//    a. Update Labels
//    b. Update Annotations
// 3. Update UserPort
func TestRunLatestService(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestRunLatestService")

	names := test.ResourceNames{
		Service: test.AppendRandomString("test-run-latest-service-", logger),
		Image:   pizzaPlanet1,
	}

	// Clean up on test failure or interrupt
	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	// Setup initial Service
	objects, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation

	if err = validateRunLatestControlPlane(logger, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(logger, clients, names, pizzaPlanetText1); err != nil {
		t.Error(err)
	}

	if err = validateLabelsPropagation(logger, *objects, names); err != nil {
		t.Error(err)
	}

	// We start a background prober to test if Route is always healthy even during Route update.
	prober := test.RunRouteProber(logger, clients, names.Domain)
	defer test.AssertProberDefault(t, prober)

	// Update Container Image
	logger.Info("Updating the Service to use a different image.")
	names.Image = printport
	image2 := test.ImagePath(names.Image)
	if _, err := test.PatchServiceImage(logger, clients, objects.Service, image2); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, image2, err)
	}

	logger.Info("Service should reflect new revision created and ready in status.")
	names.Revision, err = test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("New image not reflected in Service: %v", err)
	}

	logger.Info("Waiting for Service to transition to Ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Error waiting for the service to become ready for the latest revision: %v", err)
	}

	// Validate State after Image Update
	if err = validateRunLatestControlPlane(logger, clients, names, "2"); err != nil {
		t.Error(err)
	}
	if err = validateRunLatestDataPlane(logger, clients, names, strconv.Itoa(v1alpha1.DefaultUserPort)); err != nil {
		t.Error(err)
	}

	// Update Metadata (Labels)
	logger.Info("Updating labels of the RevisionTemplateSpec for service %s.", names.Service)
	metadata := metav1.ObjectMeta{
		Labels: map[string]string{
			"labelX": "abc",
			"labelY": "def",
		},
	}
	if objects.Service, err = test.PatchServiceRevisionTemplateMetadata(logger, clients, objects.Service, metadata); err != nil {
		t.Fatalf("Service %s was not updated with labels in its RevisionTemplateSpec: %v", names.Service, err)
	}

	logger.Info("Waiting for the new revision to appear as LatestRevision.")
	if names.Revision, err = test.WaitForServiceLatestRevision(clients, names); err != nil {
		t.Fatalf("The Service %s was not updated with new revision %s after updating labels in its RevisionTemplateSpec: %v", names.Service, names.Revision, err)
	}

	// Update Metadata (Annotations)
	logger.Infof("Updating annotations of RevisionTemplateSpec for service %s", names.Service)
	metadata = metav1.ObjectMeta{
		Annotations: map[string]string{
			"annotationA": "123",
			"annotationB": "456",
		},
	}
	if objects.Service, err = test.PatchServiceRevisionTemplateMetadata(logger, clients, objects.Service, metadata); err != nil {
		t.Fatalf("Service %s was not updated with annotation in its RevisionTemplateSpec: %v", names.Service, err)
	}

	logger.Info("Waiting for the new revision to appear as LatestRevision.")
	names.Revision, err = test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("The new revision has not become ready in Service: %v", err)
	}

	logger.Info("Waiting for Service to transition to Ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Error waiting for the service to become ready for the latest revision: %v", err)
	}

	// Validate the Service shape.
	if err = validateRunLatestControlPlane(logger, clients, names, "4"); err != nil {
		t.Error(err)
	}
	if err = validateRunLatestDataPlane(logger, clients, names, strconv.Itoa(v1alpha1.DefaultUserPort)); err != nil {
		t.Error(err)
	}

	// Update container with user port.
	logger.Infof("Updating the port of the user container for service %s to %d", names.Service, userPort)
	desiredSvc := objects.Service.DeepCopy()
	desiredSvc.Spec.RunLatest.Configuration.RevisionTemplate.Spec.Container.Ports = []corev1.ContainerPort{{
		ContainerPort: userPort,
	}}
	if objects.Service, err = test.PatchService(logger, clients, objects.Service, desiredSvc); err != nil {
		t.Fatalf("Service %s was not updated with a new port for the user container: %v", names.Service, err)
	}

	logger.Info("Waiting for the new revision to appear as LatestRevision.")
	if names.Revision, err = test.WaitForServiceLatestRevision(clients, names); err != nil {
		t.Fatalf("The new revision has not become ready in Service: %v", err)
	}

	logger.Info("Waiting for Service to transition to Ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Error waiting for the service to become ready for the latest revision: %v", err)
	}

	// Validate Service
	if err = validateRunLatestControlPlane(logger, clients, names, "5"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(logger, clients, names, strconv.Itoa(int(userPort))); err != nil {
		t.Error(err)
	}
}

// TestReleaseService creates a Service in runLatest mode and then updates it to release mode. Once in release mode the test
// goes through Update/Validate to try different possible configurations for a release service.
// Currently tests for the following combinations
// 1. One Revision Specified, current == latest
// 2. One Revision Specified, current != latset
// 3. Two Revisions Specified, 50% rollout,  candidate == latest
// 4. Two Revisions Specified, 50% rollout, candidate != latest
func TestReleaseService(t *testing.T) {
	// Create Initial Service
	clients := setup(t)
	logger := logging.GetContextLogger("TestReleaseService")
	releaseImagePath2 := test.ImagePath(pizzaPlanet2)
	releaseImagePath3 := test.ImagePath(helloworld)
	names := test.ResourceNames{
		Service: test.AppendRandomString("test-release-service-", logger),
		Image:   pizzaPlanet1,
	}
	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	// Expected Text for different revisions.
	const (
		expectedFirstRev  = pizzaPlanetText1
		expectedSecondRev = pizzaPlanetText2
		expectedThirdRev  = helloWorldText
	)

	objects, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}
	firstRevision := names.Revision

	// One Revision Specified, current == latest.
	logger.Info("Updating Service to ReleaseType using lastCreatedRevision")
	objects.Service, err = test.PatchReleaseService(logger, clients, objects.Service, []string{firstRevision}, 0)
	if err != nil {
		t.Fatalf("Service %s was not updated to release: %v", names.Service, err)
	}

	logger.Info("Service traffic should go to the first revision and be available on two names traffic targets: 'current' and 'latest'")
	validateDomains(t, logger, clients,
		names.Domain,
		[]string{expectedFirstRev},
		[]string{"latest", "current"},
		[]string{expectedFirstRev, expectedFirstRev})

	// One Revision Specified, current != latest.
	logger.Info("Updating the Service Spec with a new image")
	if _, err := test.PatchServiceImage(logger, clients, objects.Service, releaseImagePath2); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, releaseImagePath2, err)
	}

	logger.Info("Since the Service was updated a new Revision will be created")
	if names.Revision, err = test.WaitForServiceLatestRevision(clients, names); err != nil {
		t.Fatalf("The Service %s was not updated with new revision %s: %v", names.Service, names.Revision, err)
	}
	secondRevision := names.Revision

	logger.Info("Since the Service is using release the Route will not be updated, but new revision will be available at 'latest'")
	validateDomains(t, logger, clients,
		names.Domain,
		[]string{expectedFirstRev},
		[]string{"latest", "current"},
		[]string{expectedSecondRev, expectedFirstRev})

	// Two Revisions Specified, 50% rollout, candidate == latest.
	logger.Info("Updating Service to split traffic between two revisions using Release mode")
	if objects.Service, err = test.PatchReleaseService(logger, clients, objects.Service, []string{firstRevision, secondRevision}, 50); err != nil {
		t.Fatalf("Service %s was not updated to release: %v", names.Service, err)
	}

	logger.Info("Traffic should be split between the two revisions and available on three named traffic targets, 'current', 'candidate', and 'latest'")
	validateDomains(t, logger, clients,
		names.Domain,
		[]string{expectedFirstRev, expectedSecondRev},
		[]string{"candidate", "latest", "current"},
		[]string{expectedSecondRev, expectedSecondRev, expectedFirstRev})

	// Two Revisions Specified, 50% rollout, candidate != latest.
	logger.Info("Updating the Service Spec with a new image")
	if _, err := test.PatchServiceImage(logger, clients, objects.Service, releaseImagePath3); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, releaseImagePath3, err)
	}

	logger.Info("Traffic should remain between the two images, and the new revision should be available on the named traffic target 'latest'")
	validateDomains(t, logger, clients,
		names.Domain,
		[]string{expectedFirstRev, expectedSecondRev},
		[]string{"latest", "candidate", "current"},
		[]string{expectedThirdRev, expectedSecondRev, expectedFirstRev})
}

// TODO(jonjohnsonjr): Examples of deploying from source.
