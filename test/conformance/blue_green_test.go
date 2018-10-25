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
	"strings"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	concurrentRequests = 50
	// We expect to see 100% of requests succeed for traffic sent directly to revisions.
	// This might be a bad assumption.
	minDirectPercentage = 1
	// We expect to see at least 25% of either response since we're routing 50/50.
	// This might be a bad assumption.
	minSplitPercentage = 0.25

	expectedBlue  = "What a spaceport!"
	expectedGreen = "Re-energize yourself with a slice of pepperoni!"
)

// sendRequests sends "num" requests to "domain", returning a string for each spoof.Response.Body.
func sendRequests(client spoof.Interface, domain string, num int) ([]string, error) {
	responses := make([]string, num)

	// Launch "num" requests, recording the responses we get in "responses".
	g, _ := errgroup.WithContext(context.Background())
	for i := 0; i < num; i++ {
		// We don't index into "responses" inside the goroutine to avoid a race, see #1545.
		result := &responses[i]
		g.Go(func() error {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
			if err != nil {
				return err
			}

			resp, err := client.Do(req)
			if err != nil {
				return err
			}

			*result = string(resp.Body)
			return nil
		})
	}
	return responses, g.Wait()
}

// checkResponses verifies that each "expectedResponse" is present in "actualResponses" at least "min" times.
func checkResponses(logger *logging.BaseLogger, num int, min int, domain string, expectedResponses []string, actualResponses []string) error {
	// counts maps the expected response body to the number of matching requests we saw.
	counts := make(map[string]int)
	// badCounts maps the unexpected response body to the number of matching requests we saw.
	badCounts := make(map[string]int)

	// counts := eval(
	//   SELECT body, count(*) AS total
	//   FROM $actualResponses
	//   WHERE body IN $expectedResponses
	//   GROUP BY body
	// )
	for _, ar := range actualResponses {
		expected := false
		for _, er := range expectedResponses {
			if strings.Contains(string(ar), er) {
				counts[er]++
				expected = true
			}
		}
		if !expected {
			badCounts[ar]++
		}
	}

	// Verify that we saw each entry in "expectedResponses" at least "min" times.
	// check(SELECT body FROM $counts WHERE total < $min)
	totalMatches := 0
	for _, er := range expectedResponses {
		count := counts[er]
		if count < min {
			return fmt.Errorf("domain %s failed: want min %d, got %d for response %q", domain, min, count, er)
		} else {
			logger.Infof("wanted at least %d, got %d requests for domain %s", min, count, domain)
		}
		totalMatches += count
	}
	// Verify that the total expected responses match the number of requests made.
	for badResponse, count := range badCounts {
		logger.Infof("saw unexpected response %q %d times", badResponse, count)
	}
	if totalMatches < num {
		return fmt.Errorf("saw expected responses %d times, wanted %d", totalMatches, num)
	}
	// If we made it here, the implementation conforms. Congratulations!
	return nil
}

// checkDistribution sends "num" requests to "domain", then validates that
// we see each body in "expectedResponses" at least "min" times.
func checkDistribution(logger *logging.BaseLogger, clients *test.Clients, domain string, num, min int, expectedResponses []string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return err
	}

	logger.Infof("Performing %d concurrent requests to %s", num, domain)
	actualResponses, err := sendRequests(client, domain, num)
	if err != nil {
		return err
	}

	return checkResponses(logger, num, min, domain, expectedResponses, actualResponses)
}

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
	names.Service = test.AppendRandomString("pizzaplanet", logger)

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	logger.Info("Creating a new Service in runLatest")
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
	blue.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}

	logger.Info("Updating to a Manual Service to allow configuration and route to be manually modified")
	_, err = test.UpdateManualService(logger, clients, svc)
	if err != nil {
		t.Fatalf("Failed to update Service %s: %v", names.Service, err)
	}

	logger.Infof("Updating the Configuration to use a different image")
	err = updateConfigWithImage(clients, names, imagePaths)
	if err != nil {
		t.Fatalf("Patch update for Configuration %s with new image %s failed: %v", names.Config, imagePaths[1], err)
	}

	// getNextRevisionName waits for names.Revision to change, so we set it to the blue revision and wait for the (new) green revision.
	names.Revision = blue.Revision

	logger.Infof("Since the Configuration was updated a new Revision will be created and the Configuration will be updated")
	green.Revision, err = getNextRevisionName(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the Revision for image %s: %v", names.Config, imagePaths[1], err)
	}

	// We should only need to wait until the Revision is routable,
	// i.e. Ready or Inactive.  At that point, activator could start
	// queuing requests until the Revision wakes up.  However, due to
	// #882 we are currently lumping the inactive splits and that
	// would result in 100% requests reaching Blue or Green.
	//
	// TODO: After we implement #1583 and honor the split percentage
	// for inactive cases, change this wait to allow for inactive
	// revisions as well.
	logger.Infof("Waiting for revision %q to be ready", blue.Revision)
	if err := test.WaitForRevisionState(clients.ServingClient, blue.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q still can't serve traffic: %v", blue.Revision, err)
	}
	logger.Infof("Waiting for revision %q to be ready", green.Revision)
	if err := test.WaitForRevisionState(clients.ServingClient, green.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q still can't serve traffic: %v", green.Revision, err)
	}

	// Set names for traffic targets to make them directly routable.
	blue.TrafficTarget = "blue"
	green.TrafficTarget = "green"

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
	if err := test.ProbeDomain(logger, clients, greenDomain); err != nil {
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
