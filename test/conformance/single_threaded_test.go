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
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	singleThreadedImage = "singlethreaded"
)

func TestSingleConcurrency(t *testing.T) {
	clients := setup(t)

	// add test case specific name to its own logger
	logger := logging.GetContextLogger("TestSingleConcurrency")

	imagePath := test.ImagePath(singleThreadedImage)

	names := test.ResourceNames{
		Config: test.AppendRandomString("prod", logger),
		Route:  test.AppendRandomString("pizzaplanet", logger),
	}

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	configOptions := test.Options{
		ContainerConcurrency: 1,
	}
	logger.Info("Creating a new Configuration")
	err := test.CreateConfiguration(logger, clients, names, imagePath, &configOptions)
	if err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}

	logger.Info("Creating a new Route")
	err = test.CreateRoute(logger, clients, names)
	if err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	logger.Info("The Configuration will be updated with the name of the Revision once it is created")
	revisionName, err := getNextRevisionName(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}
	names.Revision = revisionName

	logger.Infof("Waiting for revision %q to be ready", names.Revision)
	if err := test.WaitForRevisionState(clients.ServingClient, names.Revision, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q still can't serve traffic: %v", names.Revision, err)
	}

	logger.Info("When the Route reports as Ready, everything should be ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	// Ready does not actually mean Ready for a Route just yet.
	// See https://github.com/knative/serving/issues/1582
	logger.Infof("Probing domain %s", domain)
	if err := test.ProbeDomain(logger, clients, domain); err != nil {
		t.Fatalf("Error probing domain %s: %v", domain, err)
	}

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	concurrency := 5
	duration := 20 * time.Second
	logger.Infof("Maintaining %d concurrent requests for %v.", concurrency, duration)
	group, _ := errgroup.WithContext(context.Background())
	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			done := time.After(duration)
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
			if err != nil {
				return fmt.Errorf("error creating http request: %v", err)
			}

			for {
				select {
				case <-done:
					return nil
				default:
					res, err := client.Do(req)
					if err != nil {
						return fmt.Errorf("error making request %v", err)
					}
					if res.StatusCode == http.StatusInternalServerError {
						return errors.New("detected concurrent requests")
					} else if res.StatusCode != http.StatusOK {
						return fmt.Errorf("non 200 response %v", res.StatusCode)
					}
				}
			}
		})
	}
	logger.Info("Waiting for all requests to complete.")
	if err := group.Wait(); err != nil {
		t.Fatalf("Error making requests for single threaded test: %v.", err)
	}
}
