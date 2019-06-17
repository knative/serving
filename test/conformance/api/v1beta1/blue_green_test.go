// +build e2e

/*
Copyright 2019 The Knative Authors

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

package v1beta1

import (
	"context"
	"math"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/test"
	v1b1test "github.com/knative/serving/test/v1beta1"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rtesting "github.com/knative/serving/pkg/testing/v1beta1"
)

const (

	// This test uses the two pizza planet test images for the blue and green deployment.
	expectedBlue  = test.PizzaPlanetText1
	expectedGreen = test.PizzaPlanetText2
)

// TestBlueGreenRoute verifies that a route configured with a 50/50 traffic split
// between two revisions will (approximately) route traffic evenly between them.
// Also, traffic that targets revisions *directly* will be routed to the correct
// revision 100% of the time.
func TestBlueGreenRoute(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	var imagePaths []string
	imagePaths = append(imagePaths, pkgTest.ImagePath(test.PizzaPlanet1))
	imagePaths = append(imagePaths, pkgTest.ImagePath(test.PizzaPlanet2))

	var names, blue, green test.ResourceNames
	// Set Service and Image for names to create the initial service
	names.Service = test.ObjectNameForTest(t)
	names.Image = test.PizzaPlanet1

	// Set names for traffic targets to make them directly routable.
	blue.TrafficTarget = "blue"
	green.TrafficTarget = "green"

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	// Setup Initial Service
	t.Log("Creating a new Service in runLatest")
	objects, err := v1b1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// The first revision created is "blue"
	blue.Revision = names.Revision

	t.Log("Updating the Service to use a different image")
	svc, err := v1b1test.PatchService(t, clients, objects.Service, rtesting.WithServiceImage(imagePaths[1]))
	if err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, imagePaths[1], err)
	}
	objects.Service = svc

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	green.Revision, err = v1b1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, imagePaths[1], err)
	}

	t.Log("Updating RouteSpec")
	if _, err := v1b1test.UpdateServiceRouteSpec(t, clients, names, v1beta1.RouteSpec{
		Traffic: []v1beta1.TrafficTarget{{
			Tag:          blue.TrafficTarget,
			RevisionName: blue.Revision,
			Percent:      50,
		}, {
			Tag:          green.TrafficTarget,
			RevisionName: green.Revision,
			Percent:      50,
		}},
	}); err != nil {
		t.Fatalf("Failed to update Service: %v", err)
	}

	t.Log("Wait for the service domains to be ready")
	if err := v1b1test.WaitForServiceState(clients.ServingBetaClient, names.Service, v1b1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	service, err := clients.ServingBetaClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Service %s: %v", names.Service, err)
	}

	var blueDomain, greenDomain string
	for _, tt := range service.Status.Traffic {
		if tt.Tag == blue.TrafficTarget {
			// Strip prefix as WaitForEndPointState expects a domain
			// without scheme.
			blueDomain = tt.URL.Host
		}
		if tt.Tag == green.TrafficTarget {
			// Strip prefix as WaitForEndPointState expects a domain
			// without scheme.
			greenDomain = tt.URL.Host
		}
	}
	if blueDomain == "" || greenDomain == "" {
		t.Fatalf("Unable to fetch URLs from traffic targets: %#v", service.Status.Traffic)
	}
	tealDomain := service.Status.URL.Host

	// Istio network programming takes some time to be effective.  Currently Istio
	// does not expose a Status, so we rely on probes to know when they are effective.
	// Since we are updating the service the teal domain probe will succeed before our changes
	// take effect so we probe the green domain.
	t.Logf("Probing domain %s", greenDomain)
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		greenDomain,
		v1b1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing domain %s: %v", greenDomain, err)
	}

	// Send concurrentRequests to blueDomain, greenDomain, and tealDomain.
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		min := int(math.Floor(test.ConcurrentRequests * test.MinSplitPercentage))
		return checkDistribution(t, clients, tealDomain, test.ConcurrentRequests, min, []string{expectedBlue, expectedGreen})
	})
	g.Go(func() error {
		min := int(math.Floor(test.ConcurrentRequests * test.MinDirectPercentage))
		return checkDistribution(t, clients, blueDomain, test.ConcurrentRequests, min, []string{expectedBlue})
	})
	g.Go(func() error {
		min := int(math.Floor(test.ConcurrentRequests * test.MinDirectPercentage))
		return checkDistribution(t, clients, greenDomain, test.ConcurrentRequests, min, []string{expectedGreen})
	})
	if err := g.Wait(); err != nil {
		t.Fatalf("Error sending requests: %v", err)
	}
}
