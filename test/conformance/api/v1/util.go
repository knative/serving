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
	"fmt"
	"math"
	"net/http"
	"net/url"
	"testing"

	"golang.org/x/sync/errgroup"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"
)

func checkForExpectedResponses(ctx context.Context, t pkgTest.TLegacy, clients *test.Clients, url *url.URL, expectedResponses ...string) error {
	client, err := pkgTest.NewSpoofingClient(ctx, clients.KubeClient, t.Logf, url.Hostname(), test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(ctx, t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return err
	}
	_, err = client.Poll(req, v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesAllBodies(expectedResponses...))))
	return err
}

func validateDomains(t pkgTest.TLegacy, clients *test.Clients, baseDomain *url.URL,
	baseExpected, trafficTargets, targetsExpected []string) error {
	subdomains := make([]*url.URL, len(trafficTargets))
	for i, target := range trafficTargets {
		subdomain, _ := url.Parse(baseDomain.String())
		subdomain.Host = target + "-" + baseDomain.Host
		subdomains[i] = subdomain
	}

	g, egCtx := errgroup.WithContext(context.Background())
	// We don't have a good way to check if the route is updated so we will wait until a subdomain has
	// started returning at least one expected result to key that we should validate percentage splits.
	// In order for tests to succeed reliably, we need to make sure that all domains succeed.
	g.Go(func() error {
		t.Log("Checking updated route", baseDomain)
		return checkForExpectedResponses(egCtx, t, clients, baseDomain, baseExpected...)
	})
	for i, s := range subdomains {
		i, s := i, s
		g.Go(func() error {
			t.Log("Checking updated route tags", s)
			return checkForExpectedResponses(egCtx, t, clients, s, targetsExpected[i])
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("error with initial domain probing: %w", err)
	}

	g.Go(func() error {
		minBasePercentage := test.MinSplitPercentage
		if len(baseExpected) == 1 {
			minBasePercentage = test.MinDirectPercentage
		}
		min := int(math.Floor(test.ConcurrentRequests * minBasePercentage))
		return shared.CheckDistribution(egCtx, t, clients, baseDomain, test.ConcurrentRequests, min, baseExpected)
	})
	for i, subdomain := range subdomains {
		i, subdomain := i, subdomain
		g.Go(func() error {
			min := int(math.Floor(test.ConcurrentRequests * test.MinDirectPercentage))
			return shared.CheckDistribution(egCtx, t, clients, subdomain, test.ConcurrentRequests, min, []string{targetsExpected[i]})
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("error checking routing distribution: %w", err)
	}
	return nil
}

// Validates service health and vended content match for a runLatest Service.
// The checks in this method should be able to be performed at any point in a
// runLatest Service's lifecycle so long as the service is in a "Ready" state.
func validateDataPlane(t pkgTest.TLegacy, clients *test.Clients, names test.ResourceNames, expectedText string) error {
	t.Log("Checking that the endpoint vends the expected text:", expectedText)
	_, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		return fmt.Errorf("the endpoint for Route %s at %s didn't serve the expected text %q: %w", names.Route, names.URL, expectedText, err)
	}

	return nil
}

// Validates the state of Configuration, Revision, and Route objects for a runLatest Service.
// The checks in this method should be able to be performed at any point in a
// runLatest Service's lifecycle so long as the service is in a "Ready" state.
func validateControlPlane(t *testing.T, clients *test.Clients, names test.ResourceNames, expectedGeneration string) error {
	t.Log("Checking to ensure Revision is in desired state with", "generation", expectedGeneration)
	err := v1test.CheckRevisionState(clients.ServingClient, names.Revision, func(r *v1.Revision) (bool, error) {
		if ready, err := v1test.IsRevisionReady(r); !ready {
			return false, fmt.Errorf("revision %s did not become ready to serve traffic: %w", names.Revision, err)
		}
		if validDigest, err := shared.ValidateImageDigest(t, names.Image, r.Status.DeprecatedImageDigest); !validDigest {
			return false, fmt.Errorf("imageDigest %s is not valid for imageName %s: %w", r.Status.DeprecatedImageDigest, names.Image, err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	err = v1test.CheckRevisionState(clients.ServingClient, names.Revision, v1test.IsRevisionAtExpectedGeneration(expectedGeneration))
	if err != nil {
		return fmt.Errorf("revision %s did not have an expected annotation with generation %s: %w", names.Revision, expectedGeneration, err)
	}

	t.Log("Checking to ensure Configuration is in desired state.")
	err = v1test.CheckConfigurationState(clients.ServingClient, names.Config, func(c *v1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
			return false, fmt.Errorf("Configuration(%s).LatestCreatedRevisionName = %q, want %q",
				names.Config, c.Status.LatestCreatedRevisionName, names.Revision)
		}
		if c.Status.LatestReadyRevisionName != names.Revision {
			return false, fmt.Errorf("Configuration(%s).LatestReadyRevisionName = %q, want %q",
				names.Config, c.Status.LatestReadyRevisionName, names.Revision)
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	t.Log("Checking to ensure Route is in desired state with", "generation", expectedGeneration)
	err = v1test.CheckRouteState(clients.ServingClient, names.Route, v1test.AllRouteTrafficAtRevision(names))
	if err != nil {
		return fmt.Errorf("the Route %s was not updated to route traffic to the Revision %s: %w", names.Route, names.Revision, err)
	}

	return nil
}

// Validates labels on Revision, Configuration, and Route objects when created by a Service
// see spec here: https://github.com/knative/serving/blob/master/docs/spec/spec.md#revision
func validateLabelsPropagation(t pkgTest.T, objects v1test.ResourceObjects, names test.ResourceNames) error {
	t.Log("Validate Labels on Revision Object")
	revision := objects.Revision

	if revision.Labels["serving.knative.dev/configuration"] != names.Config {
		return fmt.Errorf("expect Confguration name in Revision label %q but got %q ", names.Config, revision.Labels["serving.knative.dev/configuration"])
	}
	if revision.Labels["serving.knative.dev/service"] != names.Service {
		return fmt.Errorf("expect Service name in Revision label %q but got %q ", names.Service, revision.Labels["serving.knative.dev/service"])
	}

	t.Log("Validate Labels on Configuration Object")
	config := objects.Config
	if config.Labels["serving.knative.dev/service"] != names.Service {
		return fmt.Errorf("expect Service name in Configuration label %q but got %q ", names.Service, config.Labels["serving.knative.dev/service"])
	}
	if config.Labels["serving.knative.dev/route"] != names.Route {
		return fmt.Errorf("expect Route name in Configuration label %q but got %q ", names.Route, config.Labels["serving.knative.dev/route"])
	}

	t.Log("Validate Labels on Route Object")
	route := objects.Route
	if route.Labels["serving.knative.dev/service"] != names.Service {
		return fmt.Errorf("expect Service name in Route label %q but got %q ", names.Service, route.Labels["serving.knative.dev/service"])
	}
	return nil
}

func validateAnnotations(objs *v1test.ResourceObjects, extraKeys ...string) error {
	// This checks whether the annotations are set on the resources that
	// expect them to have.
	// List of issues listing annotations that we check: #1642.

	anns := objs.Service.GetAnnotations()
	for _, a := range append([]string{serving.CreatorAnnotation, serving.UpdaterAnnotation}, extraKeys...) {
		if got := anns[a]; got == "" {
			return fmt.Errorf("service expected %s annotation to be set, but was empty", a)
		}
	}
	anns = objs.Route.GetAnnotations()
	for _, a := range append([]string{serving.CreatorAnnotation, serving.UpdaterAnnotation}, extraKeys...) {
		if got := anns[a]; got == "" {
			return fmt.Errorf("route expected %s annotation to be set, but was empty", a)
		}
	}
	anns = objs.Config.GetAnnotations()
	for _, a := range append([]string{serving.CreatorAnnotation, serving.UpdaterAnnotation}, extraKeys...) {
		if got := anns[a]; got == "" {
			return fmt.Errorf("config expected %s annotation to be set, but was empty", a)
		}
	}
	return nil
}

func validateReleaseServiceShape(objs *v1test.ResourceObjects) error {
	// Traffic should be routed to the lastest created revision.
	if got, want := objs.Service.Status.Traffic[0].RevisionName, objs.Config.Status.LatestReadyRevisionName; got != want {
		return fmt.Errorf("Status.Traffic[0].RevisionsName = %s, want: %s", got, want)
	}
	return nil
}
