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
	"net/url"
	"testing"

	pkgTest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	rtesting "knative.dev/serving/pkg/testing/v1"
)

func assertResourcesUpdatedWhenRevisionIsReady(t *testing.T, clients *test.Clients, names test.ResourceNames, url *url.URL, expectedGeneration, expectedText string) {
	t.Log("When the Route reports as Ready, everything should be ready.")
	if err := v1test.WaitForRouteState(clients.ServingClient, names.Route, v1test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic to Revision %s: %v", names.Route, names.Revision, err)
	}

	// TODO(#1178): Remove "Wait" from all checks below this point.
	t.Log("Serves the expected data at the endpoint")

	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
	if err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, url, expectedText, err)
	}

	// We want to verify that the endpoint works as soon as Ready: True, but there are a bunch of other pieces of state that we validate for conformance.
	t.Log("The Revision will be marked as Ready when it can serve traffic")
	err = v1test.CheckRevisionState(clients.ServingClient, names.Revision, v1test.IsRevisionReady)
	if err != nil {
		t.Fatalf("Revision %s did not become ready to serve traffic: %v", names.Revision, err)
	}
	t.Log("The Revision will be annotated with the generation")
	err = v1test.CheckRevisionState(clients.ServingClient, names.Revision, v1test.IsRevisionAtExpectedGeneration(expectedGeneration))
	if err != nil {
		t.Fatalf("Revision %s did not have an expected annotation with generation %s: %v", names.Revision, expectedGeneration, err)
	}
	t.Log("Updates the Configuration that the Revision is ready")
	err = v1test.CheckConfigurationState(clients.ServingClient, names.Config, func(c *v1.Configuration) (bool, error) {
		return c.Status.LatestReadyRevisionName == names.Revision, nil
	})
	if err != nil {
		t.Fatalf("The Configuration %s was not updated indicating that the Revision %s was ready: %v", names.Config, names.Revision, err)
	}
	t.Log("Updates the Route to route traffic to the Revision")
	if err := v1test.WaitForRouteState(clients.ServingClient, names.Route, v1test.AllRouteTrafficAtRevision(names), "AllRouteTrafficAtRevision"); err != nil {
		t.Fatalf("The Route %s was not updated to route traffic to the Revision %s: %v", names.Route, names.Revision, err)
	}
}

func getRouteURL(clients *test.Clients, names test.ResourceNames) (*url.URL, error) {
	var url *url.URL

	err := v1test.WaitForRouteState(
		clients.ServingClient,
		names.Route,
		func(r *v1.Route) (bool, error) {
			if r.Status.URL == nil {
				return false, nil
			}
			url = r.Status.URL.URL()
			return url != nil, nil
		},
		"RouteURL",
	)

	return url, err
}

func TestRouteCreation(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	var objects v1test.ResourceObjects
	svcName := test.ObjectNameForTest(t)
	names := test.ResourceNames{
		Config:        svcName,
		Route:         svcName,
		TrafficTarget: svcName,
		Image:         test.PizzaPlanet1,
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Route and Configuration")
	config, err := v1test.CreateConfiguration(t, clients, names)
	if err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}
	objects.Config = config

	route, err := v1test.CreateRoute(t, clients, names)
	if err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}
	objects.Route = route

	t.Log("The Configuration will be updated with the name of the Revision")
	names.Revision, err = v1test.WaitForConfigLatestPinnedRevision(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}

	url, err := getRouteURL(clients, names)
	if err != nil {
		t.Fatalf("Failed to get URL from route %s: %v", names.Route, err)
	}

	t.Logf("The Route URL is: %s", url)
	assertResourcesUpdatedWhenRevisionIsReady(t, clients, names, url, "1", test.PizzaPlanetText1)

	// We start a prober at background thread to test if Route is always healthy even during Route update.
	prober := test.RunRouteProber(t.Logf, clients, url, test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
	defer test.AssertProberDefault(t, prober)

	t.Log("Updating the Configuration to use a different image")
	objects.Config, err = v1test.PatchConfig(t, clients, objects.Config, rtesting.WithConfigImage(pkgTest.ImagePath(test.PizzaPlanet2)))
	if err != nil {
		t.Fatalf("Patch update for Configuration %s with new image %s failed: %v", names.Config, test.PizzaPlanet2, err)
	}

	t.Log("Since the Configuration was updated a new Revision will be created and the Configuration will be updated")
	names.Revision, err = v1test.WaitForConfigLatestPinnedRevision(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the Revision for image %s: %v", names.Config, test.PizzaPlanet2, err)
	}

	assertResourcesUpdatedWhenRevisionIsReady(t, clients, names, url, "2", test.PizzaPlanetText2)
}
