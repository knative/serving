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

package e2e

import (
	"net/http"
	"strings"
	"testing"

	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingnetwork "knative.dev/serving/pkg/network"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestRoutesNotReady tests the scenario that when Route's status is
// Ready == False, the Service's RoutesReady value should change from
// Unknown to False
func TestRoutesNotReady(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	test.EnsureTearDown(t, clients, names)

	withTrafficSpec := rtesting.WithRouteSpec(v1.RouteSpec{
		Traffic: []v1.TrafficTarget{
			{
				RevisionName: "foobar", // Invalid revision name. This allows Revision creation to succeed and Route configuration to fail
				Percent:      ptr.Int64(100),
			},
		},
	})

	t.Log("Creating a new Service with an invalid traffic target.")
	svc, err := v1test.CreateService(t, clients, names, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service %q: %#v", names.Service, err)
	}

	t.Logf("Waiting for Service %q ObservedGeneration to match Generation, and status transition to Ready == False.", names.Service)
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceFailed, "ServiceIsNotReady"); err != nil {
		t.Fatalf("Failed waiting for Service %q to transition to Ready == False: %#v", names.Service, err)
	}

	t.Logf("Validating Route %q has reconciled to Ready == False.", serviceresourcenames.Route(svc))
	// Check Route is not ready
	if err = v1test.CheckRouteState(clients.ServingClient, serviceresourcenames.Route(svc), v1test.IsRouteFailed); err != nil {
		t.Fatalf("The Route %q was marked as Ready to serve traffic but it should not be: %#v", serviceresourcenames.Route(svc), err)
	}

	// Wait for RoutesReady to become False
	t.Logf("Validating Service %q has reconciled to RoutesReady == False.", names.Service)
	if err = v1test.CheckServiceState(clients.ServingClient, names.Service, v1test.IsServiceRoutesNotReady); err != nil {
		t.Fatalf("Service %q was not marked RoutesReady  == False: %#v", names.Service, err)
	}
}

func TestRouteVisibilityChanges(t *testing.T) {
	testCases := []struct {
		name            string
		withTrafficSpec rtesting.ServiceOption
	}{
		{
			name: "Route visibility changes from public to private with single traffic",
			withTrafficSpec: rtesting.WithRouteSpec(v1.RouteSpec{
				Traffic: []v1.TrafficTarget{
					{
						Percent: ptr.Int64(100),
					},
				},
			}),
		},
		{
			name: "Route visibility changes from public to private with tag only",
			withTrafficSpec: rtesting.WithRouteSpec(v1.RouteSpec{
				Traffic: []v1.TrafficTarget{
					{
						Percent: ptr.Int64(100),
						Tag:     "cow",
					},
				},
			}),
		},
		{
			name: "Route visibility changes from public to private with both tagged and non-tagged traffic",
			withTrafficSpec: rtesting.WithRouteSpec(v1.RouteSpec{
				Traffic: []v1.TrafficTarget{
					{
						Percent: ptr.Int64(60),
					},
					{
						Percent: ptr.Int64(40),
						Tag:     "cow",
					},
				},
			}),
		},
	}

	hasPublicRoute := func(r *v1.Route) (b bool, e error) {
		return !strings.HasSuffix(r.Status.URL.Host, network.GetClusterDomainName()), nil
	}

	hasPrivateRoute := func(r *v1.Route) (b bool, e error) {
		return strings.HasSuffix(r.Status.URL.Host, network.GetClusterDomainName()), nil
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(st *testing.T) {
			st.Parallel()
			cancel := logstream.Start(st)
			defer cancel()

			clients := Setup(st)

			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.PizzaPlanet1,
			}

			test.EnsureTearDown(t, clients, names)

			st.Log("Creating a new Service")
			svc, err := v1test.CreateService(st, clients, names, testCase.withTrafficSpec)
			if err != nil {
				st.Fatalf("Failed to create initial Service %q: %#v", names.Service, err)
			}

			st.Logf("Waiting for Service %q ObservedGeneration to match Generation, and status transition to Ready == True", names.Service)
			if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
				st.Fatalf("Failed waiting for Service %q to transition to Ready == True: %#v", names.Service, err)
			}

			st.Logf("Validating Route %q has non cluster-local address", serviceresourcenames.Route(svc))
			// Check Route is not ready

			if err = v1test.CheckRouteState(clients.ServingClient, serviceresourcenames.Route(svc), hasPublicRoute); err != nil {
				st.Fatalf("The Route %q should be publicly visible but it was not: %#v", serviceresourcenames.Route(svc), err)
			}

			v1test.PatchService(st, clients, svc, func(s *v1.Service) {
				s.SetLabels(map[string]string{"serving.knative.dev/visibility": "cluster-local"})
			})

			st.Logf("Waiting for Service %q ObservedGeneration to match Generation, and status transition to Ready == True", names.Service)
			if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
				st.Fatalf("Failed waiting for Service %q to transition to Ready == True: %#v", names.Service, err)
			}

			st.Logf("Validating Route %q has cluster-local address", serviceresourcenames.Route(svc))
			// Check Route is not ready

			if err = v1test.WaitForRouteState(clients.ServingClient, serviceresourcenames.Route(svc), hasPrivateRoute, "RouteIsClusterLocal"); err != nil {
				st.Fatalf("The Route %q should be privately visible but it was not: %#v", serviceresourcenames.Route(svc), err)
			}
		})
	}
}

// This test creates a Knative Service with two revisions: revision 1 and revision 2.
// Revision 1 has tag "rev1".
// Revision 2 does not have tag. And the Knative Service routes 100% traffic to revision 2.
// In the test, two requests will be sent to the Knative Service:
// 1) request with header "Knative-Serving-Tag: rev1". This request should be routed to revision 1.
// 2) request with header "Knative-Serving-Tag: WrongHeader". This request should be routed to revision 2
// as when header Knative-Serving-Tag does not match any revision tag, the request will fall back to the main Route.
// In order to run this test, the tag hearder based routing feature needs to be turned on:
// https://github.com/knative/serving/blob/master/config/core/configmaps/network.yaml#L115
func TestTagHeaderBasedRouting(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	// Setup Initial Service
	t.Log("Creating a new Service in runLatest")
	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	test.EnsureTearDown(t, clients, names)

	revision1 := names.Revision

	// Create a new revision with different image
	t.Log("Updating the Service to use a different image")
	service, err := v1test.PatchService(t, clients, objects.Service, rtesting.WithServiceImage(pkgTest.ImagePath(test.PizzaPlanet2)))
	if err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, test.PizzaPlanet2, err)
	}
	objects.Service = service

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	revision2, err := v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, test.PizzaPlanet2, err)
	}

	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	if _, err := v1test.UpdateServiceRouteSpec(t, clients, names, v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			Tag:          "rev1",
			RevisionName: revision1,
			Percent:      ptr.Int64(0),
		}, {
			RevisionName: revision2,
			Percent:      ptr.Int64(100),
		}},
	}); err != nil {
		t.Fatal("Failed to update Service:", err)
	}

	testCases := []struct {
		name         string
		header       string
		wantResponse string
	}{{
		name:         "send request with correct header",
		header:       "rev1",
		wantResponse: test.PizzaPlanetText1,
	}, {
		name:         "send request with non-exist header",
		header:       "rev-wrong",
		wantResponse: test.PizzaPlanetText2,
	}, {
		name:         "send request without  header",
		wantResponse: test.PizzaPlanetText2,
	}}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pkgTest.WaitForEndpointState(
				clients.KubeClient,
				t.Logf,
				objects.Service.Status.URL.URL(),
				v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(tt.wantResponse))),
				"WaitForSuccessfulResponse",
				test.ServingFlags.ResolvableDomain,
				test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
				addHeader(tt.header),
			); err != nil {
				t.Fatalf("Error probing %s: %v", objects.Service.Status.URL.URL(), err)
			}
		})
	}
}

func addHeader(header string) pkgTest.RequestOption {
	return func(req *http.Request) {
		if len(header) != 0 {
			req.Header.Add(servingnetwork.TagHeaderName, header)
		}
	}
}
