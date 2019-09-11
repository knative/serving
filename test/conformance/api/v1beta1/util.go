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
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/test"
	v1b1test "knative.dev/serving/test/v1beta1"
)

func waitForExpectedResponse(t *testing.T, clients *test.Clients, domain, expectedResponse string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return err
	}
	_, err = client.Poll(req, v1b1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedResponse))))
	return err
}

func validateDomains(
	t *testing.T, clients *test.Clients, baseDomain string,
	baseExpected, trafficTargets, targetsExpected []string) error {
	var subdomains []string
	for _, target := range trafficTargets {
		subdomains = append(subdomains, target+"-"+baseDomain)
	}

	g, _ := errgroup.WithContext(context.Background())
	// We don't have a good way to check if the route is updated so we will wait until a subdomain has
	// started returning at least one expected result to key that we should validate percentage splits.
	// In order for tests to succeed reliably, we need to make sure that all domains succeed.
	for _, resp := range baseExpected {
		// Check for each of the responses we expect from the base domain.
		resp := resp
		g.Go(func() error {
			t.Logf("Waiting for route to update domain: %s", baseDomain)
			return waitForExpectedResponse(t, clients, baseDomain, resp)
		})
	}
	for i, s := range subdomains {
		i, s := i, s
		g.Go(func() error {
			t.Logf("Waiting for route to update domain: %s", s)
			return waitForExpectedResponse(t, clients, s, targetsExpected[i])
		})
	}
	if err := g.Wait(); err != nil {
		return errors.Wrap(err, "error with initial domain probing")
	}

	g.Go(func() error {
		minBasePercentage := test.MinSplitPercentage
		if len(baseExpected) == 1 {
			minBasePercentage = test.MinDirectPercentage
		}
		min := int(math.Floor(test.ConcurrentRequests * minBasePercentage))
		return checkDistribution(t, clients, baseDomain, test.ConcurrentRequests, min, baseExpected)
	})
	for i, subdomain := range subdomains {
		i, subdomain := i, subdomain
		g.Go(func() error {
			min := int(math.Floor(test.ConcurrentRequests * test.MinDirectPercentage))
			return checkDistribution(t, clients, subdomain, test.ConcurrentRequests, min, []string{targetsExpected[i]})
		})
	}
	if err := g.Wait(); err != nil {
		return errors.Wrap(err, "error checking routing distribution")
	}
	return nil
}

// checkDistribution sends "num" requests to "domain", then validates that
// we see each body in "expectedResponses" at least "min" times.
func checkDistribution(t *testing.T, clients *test.Clients, domain string, num, min int, expectedResponses []string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return err
	}

	t.Logf("Performing %d concurrent requests to %s", num, domain)
	actualResponses, err := sendRequests(client, domain, num)
	if err != nil {
		return err
	}

	return checkResponses(t, num, min, domain, expectedResponses, actualResponses)
}

// checkResponses verifies that each "expectedResponse" is present in "actualResponses" at least "min" times.
func checkResponses(t *testing.T, num int, min int, domain string, expectedResponses []string, actualResponses []string) error {
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
			if strings.Contains(ar, er) {
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
			return fmt.Errorf("domain %s failed: want at least %d, got %d for response %q", domain, min, count, er)
		}

		t.Logf("For domain %s: wanted at least %d, got %d requests.", domain, min, count)
		totalMatches += count
	}
	// Verify that the total expected responses match the number of requests made.
	for badResponse, count := range badCounts {
		t.Logf("Saw unexpected response %q %d times.", badResponse, count)
	}
	if totalMatches < num {
		return fmt.Errorf("domain %s: saw expected responses %d times, wanted %d", domain, totalMatches, num)
	}
	// If we made it here, the implementation conforms. Congratulations!
	return nil
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

// Validates service health and vended content match for a runLatest Service.
// The checks in this method should be able to be performed at any point in a
// runLatest Service's lifecycle so long as the service is in a "Ready" state.
func validateDataPlane(t *testing.T, clients *test.Clients, names test.ResourceNames, expectedText string) error {
	t.Logf("Checking that the endpoint vends the expected text: %s", expectedText)
	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		names.Domain,
		v1b1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		return fmt.Errorf("the endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, names.Domain, expectedText, err)
	}

	return nil
}

// Validates the state of Configuration, Revision, and Route objects for a runLatest Service.
// The checks in this method should be able to be performed at any point in a
// runLatest Service's lifecycle so long as the service is in a "Ready" state.
func validateControlPlane(t *testing.T, clients *test.Clients, names test.ResourceNames, expectedGeneration string) error {
	t.Log("Checking to ensure Revision is in desired state with generation: ", expectedGeneration)
	err := v1b1test.CheckRevisionState(clients.ServingBetaClient, names.Revision, func(r *v1beta1.Revision) (bool, error) {
		if ready, err := v1b1test.IsRevisionReady(r); !ready {
			return false, fmt.Errorf("revision %s did not become ready to serve traffic: %v", names.Revision, err)
		}
		if r.Status.ImageDigest == "" {
			return false, fmt.Errorf("imageDigest not present for revision %s", names.Revision)
		}
		if validDigest, err := validateImageDigest(names.Image, r.Status.ImageDigest); !validDigest {
			return false, fmt.Errorf("imageDigest %s is not valid for imageName %s: %v", r.Status.ImageDigest, names.Image, err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	err = v1b1test.CheckRevisionState(clients.ServingBetaClient, names.Revision, v1b1test.IsRevisionAtExpectedGeneration(expectedGeneration))
	if err != nil {
		return fmt.Errorf("revision %s did not have an expected annotation with generation %s: %v", names.Revision, expectedGeneration, err)
	}

	t.Log("Checking to ensure Configuration is in desired state.")
	err = v1b1test.CheckConfigurationState(clients.ServingBetaClient, names.Config, func(c *v1beta1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
			return false, fmt.Errorf("the Configuration %s was not updated indicating that the Revision %s was created: %v", names.Config, names.Revision, err)
		}
		if c.Status.LatestReadyRevisionName != names.Revision {
			return false, fmt.Errorf("the Configuration %s was not updated indicating that the Revision %s was ready: %v", names.Config, names.Revision, err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	t.Log("Checking to ensure Route is in desired state with generation: ", expectedGeneration)
	err = v1b1test.CheckRouteState(clients.ServingBetaClient, names.Route, v1b1test.AllRouteTrafficAtRevision(names))
	if err != nil {
		return fmt.Errorf("the Route %s was not updated to route traffic to the Revision %s: %v", names.Route, names.Revision, err)
	}

	return nil
}

// Validates labels on Revision, Configuration, and Route objects when created by a Service
// see spec here: https://knative.dev/serving/blob/master/docs/spec/spec.md#revision
func validateLabelsPropagation(t *testing.T, objects v1b1test.ResourceObjects, names test.ResourceNames) error {
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

func validateAnnotations(objs *v1b1test.ResourceObjects, extraKeys ...string) error {
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

func validateReleaseServiceShape(objs *v1b1test.ResourceObjects) error {
	// Traffic should be routed to the lastest created revision.
	if got, want := objs.Service.Status.Traffic[0].RevisionName, objs.Config.Status.LatestReadyRevisionName; got != want {
		return fmt.Errorf("Status.Traffic[0].RevisionsName = %s, want: %s", got, want)
	}
	return nil
}

func validateImageDigest(imageName string, imageDigest string) (bool, error) {
	imageDigestRegex := fmt.Sprintf("%s/%s@sha256:[0-9a-f]{64}", pkgTest.Flags.DockerRepo, imageName)
	return regexp.MatchString(imageDigestRegex, imageDigest)
}
