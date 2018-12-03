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
	"testing"

	"context"
	"fmt"
	"math"
	"net/http"
	"regexp"

	pkgTest "github.com/knative/pkg/test"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	"golang.org/x/sync/errgroup"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// Constants for test images located in test/test_images
const (
	pizzaPlanet1        = "pizzaplanetv1"
	pizzaPlanetText1    = "What a spaceport!"
	pizzaPlanet2        = "pizzaplanetv2"
	pizzaPlanetText2    = "Re-energize yourself with a slice of pepperoni!"
	helloworld          = "helloworld"
	helloWorldText      = "Hello World! How about some tasty noodles?"
	httpproxy           = "httpproxy"
	singleThreadedImage = "singlethreaded"
	timeout             = "timeout"
)

func setup(t *testing.T) *test.Clients {
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

func tearDown(clients *test.Clients, names test.ResourceNames) {
	if clients != nil && clients.ServingClient != nil {
		clients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}
}

func waitForExpectedResponse(logger *logging.BaseLogger, clients *test.Clients, domain, expectedResponse string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return err
	}
	_, err = client.Poll(req, pkgTest.EventuallyMatchesBody(expectedResponse))
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
	err := waitForExpectedResponse(logger, clients, subdomains[0], targetsExpected[0])
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

func validateImageDigest(imageName string, imageDigest string) (bool, error) {
	imageDigestRegex := fmt.Sprintf("%s/%s@sha256:[0-9a-f]{64}", test.ServingFlags.DockerRepo, imageName)
	match, err := regexp.MatchString(imageDigestRegex, imageDigest)
	if err != nil {
		return false, err
	}
	if !match {
		return false, nil
	}
	return true, nil
}
