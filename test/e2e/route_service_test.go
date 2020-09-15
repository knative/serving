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
	"strings"
	"testing"

	netpkg "knative.dev/networking/pkg"
	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
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

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	test.EnsureTearDown(t, clients, &names)

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

			clients := Setup(st)

			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.PizzaPlanet1,
			}

			test.EnsureTearDown(t, clients, &names)

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
				s.SetLabels(map[string]string{netpkg.VisibilityLabelKey: "cluster-local"})
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
