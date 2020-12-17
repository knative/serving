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

package v1

import (
	"context"
	"math"
	"net/url"
	"testing"

	"golang.org/x/sync/errgroup"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"
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

	// Set Service and Image for names to create the initial service
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	greenImagePath := pkgTest.ImagePath(test.PizzaPlanet2)

	test.EnsureTearDown(t, clients, &names)

	// Setup Initial Service
	t.Log("Creating a new Service in runLatest")
	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// The first revision created is "blue"
	blue := names
	blue.TrafficTarget = "blue"
	green := names
	green.TrafficTarget = "green"

	t.Log("Updating the Service to use a different image")
	service, err := v1test.PatchService(t, clients, objects.Service, rtesting.WithServiceImage(greenImagePath))
	if err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, greenImagePath, err)
	}
	objects.Service = service

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	green.Revision, err = v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, greenImagePath, err)
	}

	t.Log("Updating RouteSpec")
	if _, err := v1test.UpdateServiceRouteSpec(t, clients, names, v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			Tag:          blue.TrafficTarget,
			RevisionName: blue.Revision,
			Percent:      ptr.Int64(50),
		}, {
			Tag:          green.TrafficTarget,
			RevisionName: green.Revision,
			Percent:      ptr.Int64(50),
		}},
	}); err != nil {
		t.Fatal("Failed to update Service:", err)
	}

	t.Log("Wait for the service domains to be ready")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	service, err = clients.ServingClient.Services.Get(context.Background(), names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Service %s: %v", names.Service, err)
	}

	var blueURL, greenURL *url.URL
	for _, tt := range service.Status.Traffic {
		if tt.Tag == blue.TrafficTarget {
			blueURL = tt.URL.URL()
		}
		if tt.Tag == green.TrafficTarget {
			greenURL = tt.URL.URL()
		}
	}
	if blueURL == nil || greenURL == nil {
		t.Fatalf("Unable to fetch URLs from traffic targets: %#v", service.Status.Traffic)
	}
	tealURL := service.Status.URL.URL()

	// Since we are updating the service the teal domain probe will succeed before our changes
	// take effect so we probe the green domain.
	t.Log("Probing", greenURL)
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		greenURL,
		v1test.RetryingRouteInconsistency(spoof.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS)); err != nil {
		t.Fatalf("Error probing %s: %v", greenURL, err)
	}

	// Send concurrentRequests to blueDomain, greenDomain, and tealDomain.
	g, egCtx := errgroup.WithContext(context.Background())
	g.Go(func() error {
		min := int(math.Floor(test.ConcurrentRequests * test.MinSplitPercentage))
		return shared.CheckDistribution(egCtx, t, clients, tealURL, test.ConcurrentRequests, min, []string{expectedBlue, expectedGreen}, test.ServingFlags.ResolvableDomain)
	})
	g.Go(func() error {
		min := int(math.Floor(test.ConcurrentRequests * test.MinDirectPercentage))
		return shared.CheckDistribution(egCtx, t, clients, blueURL, test.ConcurrentRequests, min, []string{expectedBlue}, test.ServingFlags.ResolvableDomain)
	})
	g.Go(func() error {
		min := int(math.Floor(test.ConcurrentRequests * test.MinDirectPercentage))
		return shared.CheckDistribution(egCtx, t, clients, greenURL, test.ConcurrentRequests, min, []string{expectedGreen}, test.ServingFlags.ResolvableDomain)
	})
	if err := g.Wait(); err != nil {
		t.Fatal("Error sending requests:", err)
	}
}
