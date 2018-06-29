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
	"sync/atomic"
	"testing"

	"github.com/knative/serving/test"
	"github.com/knative/serving/test/spoof"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	concurrentRequests = 100
	// We expect to see 100% of requests succeed for traffic sent directly to revisions.
	// This might be a bad assumption.
	minDirectPercentage = 1.0
	// We expect to see at least 25% of either response since we're routing 50/50.
	// This might be a bad assumption.
	minSplitPercentage = 0.25
)

type recordingChecker struct {
	match spoof.ResponseChecker
	// Minimum number of times we expect to match checker.
	min int32
	// Actual number of times we match checker.
	count int32
}

func makeRequests(logger *zap.SugaredLogger, clients *test.Clients, num int, domain string, checks ...*recordingChecker) error {
	client, err := spoof.New(clients.Kube, logger, domain, test.Flags.ResolvableDomain)
	if err != nil {
		return err
	}

	// TODO(#348): The ingress endpoint tends to return 503's and 404's
	client.RetryCodes = []int{http.StatusServiceUnavailable, http.StatusNotFound}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return err
	}

	// Poll until we get a successful response.
	logger.Infof("Polling %s until we get a 200", domain)
	if _, err := client.Poll(req, test.MatchesAny); err != nil {
		return err
	}

	logger.Infof("Performing %d concurrent requests to %s", num, domain)
	g, _ := errgroup.WithContext(context.Background())
	for i := 0; i < num; i++ {
		g.Go(func() error {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
			if err != nil {
				return err
			}

			// TODO(tcnghia): I still get 404 sometimes. Why do I still have to poll here? :( I want this:
			// resp, err := client.Do(req)
			resp, err := client.Poll(req, test.MatchesAny)
			if err != nil {
				return err
			}

			for _, check := range checks {
				if ok, err := check.match(resp); err != nil {
					// For multiple matching checks, we don't want an error to be fatal.
					if len(checks) == 1 {
						logger.Infof("Bad response:\n%#v", resp)
						return err
					}
				} else if ok {
					atomic.AddInt32(&check.count, 1)
				}
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	for i, check := range checks {
		if check.count < check.min {
			return fmt.Errorf("check[%d] for domain %s failed: want min %d, got %d", i, domain, check.min, check.count)
		} else {
			logger.Infof("wanted at least %d, got %d requests for %s", check.min, check.count, domain)
		}
	}

	return nil
}

func TestBlueGreenRoute(t *testing.T) {
	clients := setup(t)

	//add test case specific name to its own logger
	logger := test.Logger.Named("TestBlueGreenRoute")

	var imagePaths []string
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image1}, "/"))
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image2}, "/"))

	var names, blue, green test.ResourceNames
	names.Config = test.AppendRandomString("prod", logger)
	names.Route = test.AppendRandomString("pizzaplanet", logger)

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	logger.Infof("Creating a Configuration")
	if _, err := clients.Configs.Create(test.Configuration(test.Flags.Namespace, names, imagePaths[0])); err != nil {
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
	if err := test.WaitForRevisionState(clients.Revisions, blue.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q was not marked as Ready: %v", blue.Revision, err)
	}
	logger.Infof("Waiting for revision %q to be ready", green.Revision)
	if err := test.WaitForRevisionState(clients.Revisions, green.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q was not marked as Ready: %v", green.Revision, err)
	}

	// Set names for traffic targets to make them directly routable.
	blue.TrafficTarget = "blue"
	green.TrafficTarget = "green"

	logger.Infof("Creating a Route")
	if _, err := clients.Routes.Create(test.BlueGreenRoute(test.Flags.Namespace, names, blue, green)); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	logger.Infof("When the Route reports as Ready, everything should be ready.")
	if err := test.WaitForRouteState(clients.Routes, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}

	// TODO(tcnghia): Add a RouteStatus method to create this string.
	blueDomain := fmt.Sprintf("%s.%s", blue.TrafficTarget, route.Status.Domain)
	greenDomain := fmt.Sprintf("%s.%s", green.TrafficTarget, route.Status.Domain)
	tealDomain := route.Status.Domain

	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		return makeRequests(logger, clients, concurrentRequests, blueDomain, &recordingChecker{
			match: test.MatchesBody("What a spaceport!"),
			min:   int32(concurrentRequests * minDirectPercentage),
		})
	})
	g.Go(func() error {
		return makeRequests(logger, clients, concurrentRequests, greenDomain, &recordingChecker{
			match: test.MatchesBody("Re-energize yourself with a slice of pepperoni!"),
			min:   int32(concurrentRequests * minDirectPercentage),
		})
	})
	g.Go(func() error {
		return makeRequests(logger, clients, concurrentRequests, tealDomain,
			&recordingChecker{
				match: test.MatchesBody("What a spaceport!"),
				min:   int32(concurrentRequests * minSplitPercentage),
			},
			&recordingChecker{
				match: test.MatchesBody("Re-energize yourself with a slice of pepperoni!"),
				min:   int32(concurrentRequests * minSplitPercentage),
			})
	})
	if err := g.Wait(); err != nil {
		t.Fatalf("Error sending requests: %v", err)
	}
}
