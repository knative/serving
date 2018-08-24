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
	"net/http"
	"strings"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	concurrentRequests = 100
	// We expect to see 100% of requests succeed for traffic sent directly to revisions.
	// This might be a bad assumption.
	minDirectPercentage = 1
	// We expect to see at least 25% of either response since we're routing 50/50.
	// This might be a bad assumption.
	minSplitPercentage = 0.25

	expectedBlue  = "What a spaceport!"
	expectedGreen = "Re-energize yourself with a slice of pepperoni!"
)

// Probe until we get a successful response. This ensures the domain is
// routable before we send it a bunch of traffic.
func probeDomain(logger *logging.BaseLogger, clients *test.Clients, domain string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return err
	}
	// TODO(tcnghia): Replace this probing with Status check when we have them.
	_, err = client.Poll(req, pkgTest.Retrying(pkgTest.MatchesAny, http.StatusNotFound, http.StatusServiceUnavailable))
	return err
}

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
	imagePaths = append(imagePaths, test.ImagePath(image1))
	imagePaths = append(imagePaths, test.ImagePath(image2))

	var names, blue, green test.ResourceNames
	names.Config = test.AppendRandomString("prod", logger)
	names.Route = test.AppendRandomString("pizzaplanet", logger)

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	logger.Infof("Creating a Configuration")
	if err := test.CreateConfiguration(logger, clients, names, imagePaths[0]); err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}

	var err error

	logger.Infof("The Configuration will be updated with the name of the Revision once it is created")
	blue.Revision, err = getNextRevisionName(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
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
		t.Fatalf("Configuration %s was not updated with the Revision for image %s: %v", names.Config, image2, err)
	}

	// TODO(#882): Remove these?
	logger.Infof("Waiting for revision %q to be ready", blue.Revision)
	if err := test.WaitForRevisionState(clients.ServingClient, blue.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q was not marked as Ready: %v", blue.Revision, err)
	}
	logger.Infof("Waiting for revision %q to be ready", green.Revision)
	if err := test.WaitForRevisionState(clients.ServingClient, green.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q was not marked as Ready: %v", green.Revision, err)
	}

	// Set names for traffic targets to make them directly routable.
	blue.TrafficTarget = "blue"
	green.TrafficTarget = "green"

	logger.Infof("Creating a Route")
	if err := test.CreateBlueGreenRoute(logger, clients, names, blue, green); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	logger.Infof("When the Route reports as Ready, everything should be ready.")
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
	// It doesn't matter which domain we probe, we just need to choose one.
	if err := probeDomain(logger, clients, tealDomain); err != nil {
		t.Fatalf("Error probing domain %s: %v", tealDomain, err)
	}

	// Send concurrentRequests to blueDomain, greenDomain, and tealDomain.
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		min := int(concurrentRequests * minSplitPercentage)
		return checkDistribution(logger, clients, tealDomain, concurrentRequests, min, []string{expectedBlue, expectedGreen})
	})
	g.Go(func() error {
		min := int(concurrentRequests * minDirectPercentage)
		return checkDistribution(logger, clients, blueDomain, concurrentRequests, min, []string{expectedBlue})
	})
	g.Go(func() error {
		min := int(concurrentRequests * minDirectPercentage)
		return checkDistribution(logger, clients, greenDomain, concurrentRequests, min, []string{expectedGreen})
	})
	if err := g.Wait(); err != nil {
		t.Fatalf("Error sending requests: %v", err)
	}
}
