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
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	expectedLatest = "Hello World! How about some tasty noodles?"
)

func waitForExpected(logger *logging.BaseLogger, clients *test.Clients, domain, expected string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return err
	}
	_, err = client.Poll(req, pkgTest.EventuallyMatchesBody(expected))
	return err
}

func validateDomains(t *testing.T, logger *logging.BaseLogger, clients *test.Clients, baseDomain string, baseExpected, trafficTargets, targetsExpected []string) {
	var subdomains []string
	for _, target := range trafficTargets {
		subdomains = append(subdomains, fmt.Sprintf("%s.%s", target, baseDomain))
	}

	// We don't have a good way to check if the route is updated so we will wait until a subdomain has
	// started returning at least one expected result to key that we should validate percentage splits.
	logger.Infof("Waiting for route to update domain: %s", subdomains[0])
	err := waitForExpected(logger, clients, subdomains[0], targetsExpected[0])
	if err != nil {
		t.Fatalf("Error waiting for route to update %s: %v", subdomains[0], targetsExpected[0])
	}

	g, _ := errgroup.WithContext(context.Background())
	var minBasePercentage float64
	if len(baseExpected) == 1 {
		minBasePercentage = minDirectPercentage
	} else {
		minBasePercentage = minSplitPercentage
	}
	g.Go(func() error {
		min := int(math.Floor(concurrentRequests * minBasePercentage))
		return checkDistribution(logger, clients, baseDomain, concurrentRequests, min, baseExpected)
	})
	if err := g.Wait(); err != nil {
		t.Fatalf("Error sending requests: %v", err)
	}
	for i, subdomain := range subdomains {
		g.Go(func() error {
			min := int(math.Floor(concurrentRequests * minDirectPercentage))
			return checkDistribution(logger, clients, subdomain, concurrentRequests, min, []string{targetsExpected[i]})
		})
		// Wait before going to the next domain as to not mutate subdomain and i
		if err := g.Wait(); err != nil {
			t.Fatalf("Error sending requests: %v", err)
		}
	}

}

// Shamelessly cribbed from route_test. We expect the Route and Configuration to be ready if the Service is ready.
func assertServiceResourcesUpdated(t *testing.T, logger *logging.BaseLogger, clients *test.Clients, names test.ResourceNames, routeDomain, expectedGeneration, expectedText string) {
	// TODO(#1178): Remove "Wait" from all checks below this point.
	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		routeDomain,
		pkgTest.Retrying(pkgTest.EventuallyMatchesBody(expectedText), http.StatusNotFound),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, routeDomain, expectedText, err)
	}

	// We want to verify that the endpoint works as soon as Ready: True, but there are a bunch of other pieces of state that we validate for conformance.
	logger.Info("The Revision will be marked as Ready when it can serve traffic")
	if err := test.CheckRevisionState(clients.ServingClient, names.Revision, test.IsRevisionReady); err != nil {
		t.Fatalf("Revision %s did not become ready to serve traffic: %v", names.Revision, err)
	}
	logger.Infof("The Revision will be annotated with the generation")
	err = test.CheckRevisionState(clients.ServingClient, names.Revision, test.IsRevisionAtExpectedGeneration(expectedGeneration))
	if err != nil {
		t.Fatalf("Revision %s did not have an expected annotation with generation %s: %v", names.Revision, expectedGeneration, err)
	}
	logger.Info("The Service's latestReadyRevisionName should match the Configuration's")
	err = test.CheckConfigurationState(clients.ServingClient, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		return c.Status.LatestReadyRevisionName == names.Revision, nil
	})
	if err != nil {
		t.Fatalf("The Configuration %s was not updated indicating that the Revision %s was ready: %v\n", names.Config, names.Revision, err)
	}

	logger.Info("Updates the Route to route traffic to the Revision")
	if err := test.CheckRouteState(clients.ServingClient, names.Route, test.AllRouteTrafficAtRevision(names)); err != nil {
		t.Fatalf("The Route %s was not updated to route traffic to the Revision %s: %v", names.Route, names.Revision, err)
	}

	logger.Infof("TODO: The Service's Route is accessible from inside the cluster without external DNS")
	err = test.CheckServiceState(clients.ServingClient, names.Service, test.TODO_ServiceTrafficToRevisionWithInClusterDNS)
	if err != nil {
		t.Fatalf("The Service %s was not able to route traffic to the Revision %s with in cluster DNS: %v", names.Service, names.Revision, err)
	}

	// TODO(#1381): Check labels and annotations.
}

func waitForServiceLatestCreatedRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	var revisionName string
	err := test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
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
	err := test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
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
	logger := logging.GetContextLogger("TestRunLatestService")

	var imagePaths []string
	imagePaths = append(imagePaths, test.ImagePath(pizzaPlanet1))
	imagePaths = append(imagePaths, test.ImagePath(pizzaPlanet2))

	var names test.ResourceNames
	names.Service = test.AppendRandomString("pizzaplanet-service", logger)

	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, imagePaths[0])
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

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
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "1", "What a spaceport!")

	// We start a background prober to test if Route is always healthy even during Route update.
	routeProberErrorChan := test.RunRouteProber(logger, clients, routeDomain)

	logger.Info("Updating the Service to use a different image")
	if _, err := test.UpdateServiceImage(clients, svc, imagePaths[1]); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, imagePaths[1], err)
	}

	logger.Info("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, pizzaPlanet2, err)
	}
	names.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "2", "Re-energize yourself with a slice of pepperoni!")

	if err := test.GetRouteProberError(routeProberErrorChan, logger); err != nil {
		// Currently the Route prober is flaky. So we just log the error here for future debugging instead of
		// failing the test.
		logger.Errorf("Route prober failed with error %s", err)
	}
}

func TestUpdateRevisionTemplateSpecMetadata(t *testing.T) {
	clients := setup(t)

	logger := logging.GetContextLogger("TestUpdateRevisionTemplateSpecMetadata")

	var names test.ResourceNames
	names.Service = test.AppendRandomString("pizzaplanet-service", logger)

	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, test.ImagePath(pizzaPlanet1))
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	logger.Info("The Service will be updated with the name of the Revision once it is created")
	names.Revision, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}

	logger.Info("Updating labels of the RevisionTemplateSpec for service %s", names.Service)
	metadata := metav1.ObjectMeta{
		Labels: map[string]string{
			"labelX": "abc",
			"labelY": "def",
		},
	}
	svc, err = test.UpdateServiceRevisionTemplateMetadata(logger, clients, svc, metadata)
	if err != nil {
		t.Fatalf("Service %s was not updated with labels in its RevisionTemplateSpec: %v", names.Service, err)
	}

	names.Revision, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with new a new revision after updating labels in its RevisionTemplateSpec: %v", names.Service, err)
	}

	logger.Infof("Updating annotations of RevisionTemplateSpec for service %s", names.Service)
	metadata = metav1.ObjectMeta{
		Annotations: map[string]string{
			"annotationA": "123",
			"annotationB": "456",
		},
	}
	svc, err = test.UpdateServiceRevisionTemplateMetadata(logger, clients, svc, metadata)
	if err != nil {
		t.Fatalf("Service %s was not updated with annotation in its RevisionTemplateSpec: %v", names.Service, err)
	}

	names.Revision, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with new a new revision after updating annotations in its RevisionTemplateSpec: %v", names.Service, err)
	}

	routeDomain, err := waitForServiceDomain(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new route: %v", names.Service, err)
	}

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "3", "What a spaceport!")
}

func TestReleaseService(t *testing.T) {
	clients := setup(t)
	logger := logging.GetContextLogger("TestReleaseService")
	releaseImagePath1 := test.ImagePath(pizzaPlanet1)
	releaseImagePath2 := test.ImagePath(pizzaPlanet2)
	releaseImagePath3 := test.ImagePath(helloworld)

	names := test.ResourceNames{
		Service: test.AppendRandomString("pizzaplanet-service", logger),
	}

	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	logger.Info("Creating a new Service in runLatest")
	svc, err := test.CreateLatestService(logger, clients, names, releaseImagePath1)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	logger.Info("The Service will be updated with the name of the Revision once it is created")
	revisionName, err := waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	names.Revision = revisionName
	firstRevision := revisionName

	logger.Info("The Service will be updated with the domain of the Route once it is created")
	routeDomain, err := waitForServiceDomain(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new route: %v", names.Service, err)
	}

	logger.Info("When the Service reports as Ready, everything should be ready")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "1", "What a spaceport!")

	// Everything above here is setup to get us into a good state to test release mode
	logger.Info("Updating Service to ReleaseType using lastCreatedRevision")
	svc, err = test.UpdateReleaseService(logger, clients, svc, []string{firstRevision}, 0)
	if err != nil {
		t.Fatalf("Service %s was not updated to release: %v", names.Service, err)
	}

	logger.Info("Service traffic should go to the first revision and be available on two names traffic targets, 'current' and 'latest'")
	validateDomains(t, logger, clients,
		routeDomain,
		[]string{expectedBlue},
		[]string{"latest", "current"},
		[]string{expectedBlue, expectedBlue})

	logger.Info("Updating the Service Spec with a new image")
	if _, err := test.UpdateServiceImage(clients, svc, releaseImagePath2); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, releaseImagePath2, err)
	}

	logger.Info("Since the Service was updated a new Revision will be created")
	revisionName, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, releaseImagePath2, err)
	}
	names.Revision = revisionName
	secondRevision := revisionName

	logger.Info("Since the Service is using release the Route will not be updated, but new revision will be available at 'latest'")
	validateDomains(t, logger, clients,
		routeDomain,
		[]string{expectedBlue},
		[]string{"latest", "current"},
		[]string{expectedGreen, expectedBlue})

	logger.Info("Updating Service to split traffic between two revisions using Release mode")
	svc, err = test.UpdateReleaseService(logger, clients, svc, []string{firstRevision, secondRevision}, 50)
	if err != nil {
		t.Fatalf("Service %s was not updated to release: %v", names.Service, err)
	}

	logger.Info("Traffic should be split between the two revisions and available on three named traffic targets, 'current', 'candidate', and 'latest'")
	validateDomains(t, logger, clients,
		routeDomain,
		[]string{expectedBlue, expectedGreen},
		[]string{"candidate", "latest", "current"},
		[]string{expectedGreen, expectedGreen, expectedBlue})

	logger.Info("Updating the Service Spec with a new image")
	if _, err := test.UpdateServiceImage(clients, svc, releaseImagePath3); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, releaseImagePath3, err)
	}

	logger.Info("Traffic should remain between the two images, and the new revision should be available on the named traffic target 'latest'")
	validateDomains(t, logger, clients,
		routeDomain,
		[]string{expectedBlue, expectedGreen},
		[]string{"latest", "candidate", "current"},
		[]string{expectedLatest, expectedGreen, expectedBlue})
}

// TODO(jonjohnsonjr): LatestService roads less traveled.
// TODO(jonjohnsonjr): PinnedService happy path.
// TODO(jonjohnsonjr): PinnedService roads less traveled.
// TODO(jonjohnsonjr): Examples of deploying from source.
