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
	"github.com/knative/serving/test"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (

	// This test uses the two pizza planet test images for the blue and green deployment.
	expectedBlue  = pizzaPlanetText1
	expectedGreen = pizzaPlanetText2
)

// TestBlueGreenRoute verifies that a route configured with a 50/50 traffic split
// between two revisions will (approximately) route traffic evenly between them.
// Also, traffic that targets revisions *directly* will be routed to the correct
// revision 100% of the time.
func TestBlueGreenRoute(t *testing.T) {
	clients := setup(t)

	// add test case specific name to its own logger
	logger := logging.GetContextLogger("TestBlueGreenRoute")

	var imagePaths []string
	imagePaths = append(imagePaths, test.ImagePath(pizzaPlanet1))
	imagePaths = append(imagePaths, test.ImagePath(pizzaPlanet2))

	var names, blue, green test.ResourceNames
	// Set Service and Image for names to create the initial service
	names.Service = test.AppendRandomString("test-blue-green-route-", logger)
	names.Image = pizzaPlanet1

	// Set names for traffic targets to make them directly routable.
	blue.TrafficTarget = "blue"
	green.TrafficTarget = "green"

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	// Setup Initial Service
	logger.Info("Creating a new Service in runLatest")
	objects, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// The first revision created is "blue"
	blue.Revision = names.Revision

	logger.Info("Updating to a Manual Service to allow configuration and route to be manually modified")
	svc, err := test.PatchManualService(logger, clients, objects.Service)
	if err != nil {
		t.Fatalf("Failed to update Service %s: %v", names.Service, err)
	}
	objects.Service = svc

	logger.Infof("Updating the Configuration to use a different image")
	cfg, err := test.PatchConfigImage(logger, clients, objects.Config, imagePaths[1])
	if err != nil {
		t.Fatalf("Patch update for Configuration %s with new image %s failed: %v", names.Config, imagePaths[1], err)
	}
	objects.Config = cfg

	logger.Infof("Since the Configuration was updated a new Revision will be created and the Configuration will be updated")
	green.Revision, err = test.WaitForConfigLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the Revision for image %s: %v", names.Config, imagePaths[1], err)
	}

	logger.Infof("Updating Route")
	if _, err := test.UpdateBlueGreenRoute(logger, clients, names, blue, green); err != nil {
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

	// TODO(tcnghia): Add a RouteStatus method to create this string.
	blueDomain := fmt.Sprintf("%s.%s", blue.TrafficTarget, route.Status.Domain)
	greenDomain := fmt.Sprintf("%s.%s", green.TrafficTarget, route.Status.Domain)
	tealDomain := route.Status.Domain

	// Istio network programming takes some time to be effective.  Currently Istio
	// does not expose a Status, so we rely on probes to know when they are effective.
	// Since we are updating the route the teal domain probe will succeed before our changes
	// take effect so we probe the green domain.
	logger.Infof("Probing domain %s", greenDomain)
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		greenDomain,
		pkgTest.Retrying(pkgTest.MatchesAny, http.StatusNotFound, http.StatusServiceUnavailable),
		"nexistent key: selfLinWaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing domain %s: %v", greenDomain, err)
	}

	// Send concurrentRequests to blueDomain, greenDomain, and tealDomain.
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		min := int(math.Floor(concurrentRequests * minSplitPercentage))
		return checkDistribution(logger, clients, tealDomain, concurrentRequests, min, []string{expectedBlue, expectedGreen})
	})
	g.Go(func() error {
		min := int(math.Floor(concurrentRequests * minDirectPercentage))
		return checkDistribution(logger, clients, blueDomain, concurrentRequests, min, []string{expectedBlue})
	})
	g.Go(func() error {
		min := int(math.Floor(concurrentRequests * minDirectPercentage))
		return checkDistribution(logger, clients, greenDomain, concurrentRequests, min, []string{expectedGreen})
	})
	if err := g.Wait(); err != nil {
		t.Fatalf("Error sending requests: %v", err)
	}
}
