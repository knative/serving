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

	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/logstream"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	. "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
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

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	withTrafficSpec := WithInlineRouteSpec(v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{
			{
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "foobar", // Invalid revision name. This allows Revision creation to succeed and Route configuration to fail
					Percent:      ptr.Int64(100),
				},
			},
		},
	})

	t.Log("Creating a new Service with an invalid traffic target.")
	svc, err := v1a1test.CreateLatestService(t, clients, names, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service %q: %#v", names.Service, err)
	}

	t.Logf("Waiting for Service %q ObservedGeneration to match Generation, and status transition to Ready == False.", names.Service)
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceNotReady, "ServiceIsNotReady"); err != nil {
		t.Fatalf("Failed waiting for Service %q to transition to Ready == False: %#v", names.Service, err)
	}

	t.Logf("Validating Route %q has reconciled to Ready == False.", serviceresourcenames.Route(svc))
	// Check Route is not ready
	if err = v1a1test.CheckRouteState(clients.ServingAlphaClient, serviceresourcenames.Route(svc), v1a1test.IsRouteNotReady); err != nil {
		t.Fatalf("The Route %q was marked as Ready to serve traffic but it should not be: %#v", serviceresourcenames.Route(svc), err)
	}

	// Wait for RoutesReady to become False
	t.Logf("Validating Service %q has reconciled to RoutesReady == False.", names.Service)
	if err = v1a1test.CheckServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceRoutesNotReady); err != nil {
		t.Fatalf("Service %q was not marked RoutesReady  == False: %#v", names.Service, err)
	}
}

func TestRouteVisibilityChanges(t *testing.T) {
	testCases := []struct {
		name            string
		withTrafficSpec ServiceOption
	}{
		{
			name: "Route visibility changes from public to private with single traffic",
			withTrafficSpec: WithInlineRouteSpec(v1alpha1.RouteSpec{
				Traffic: []v1alpha1.TrafficTarget{
					{
						TrafficTarget: v1.TrafficTarget{
							Percent: ptr.Int64(100),
						},
					},
				},
			}),
		},
		{
			name: "Route visibility changes from public to private with tag only",
			withTrafficSpec: WithInlineRouteSpec(v1alpha1.RouteSpec{
				Traffic: []v1alpha1.TrafficTarget{
					{
						TrafficTarget: v1.TrafficTarget{
							Percent: ptr.Int64(100),
							Tag:     "cow",
						},
					},
				},
			}),
		},
		{
			name: "Route visibility changes from public to private with both tagged and non-tagged traffic",
			withTrafficSpec: WithInlineRouteSpec(v1alpha1.RouteSpec{
				Traffic: []v1alpha1.TrafficTarget{
					{
						TrafficTarget: v1.TrafficTarget{
							Percent: ptr.Int64(60),
						},
					},
					{
						TrafficTarget: v1.TrafficTarget{
							Percent: ptr.Int64(40),
							Tag:     "cow",
						},
					},
				},
			}),
		},
	}

	hasPublicRoute := func(r *v1alpha1.Route) (b bool, e error) {
		return !strings.HasSuffix(r.Status.URL.Host, network.GetClusterDomainName()), nil
	}

	hasPrivateRoute := func(r *v1alpha1.Route) (b bool, e error) {
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

			test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
			defer test.TearDown(clients, names)

			st.Log("Creating a new Service")
			svc, err := v1a1test.CreateLatestService(st, clients, names, testCase.withTrafficSpec)
			if err != nil {
				st.Fatalf("Failed to create initial Service %q: %#v", names.Service, err)
			}

			st.Logf("Waiting for Service %q ObservedGeneration to match Generation, and status transition to Ready == True", names.Service)
			if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
				st.Fatalf("Failed waiting for Service %q to transition to Ready == True: %#v", names.Service, err)
			}

			st.Logf("Validating Route %q has non cluster-local address", serviceresourcenames.Route(svc))
			// Check Route is not ready

			if err = v1a1test.CheckRouteState(clients.ServingAlphaClient, serviceresourcenames.Route(svc), hasPublicRoute); err != nil {
				st.Fatalf("The Route %q should be publicly visible but it was not: %#v", serviceresourcenames.Route(svc), err)
			}

			newSvc := svc.DeepCopy()
			newSvc.SetLabels(map[string]string{"serving.knative.dev/visibility": "cluster-local"})
			v1a1test.PatchService(st, clients, svc, newSvc)

			st.Logf("Waiting for Service %q ObservedGeneration to match Generation, and status transition to Ready == True", names.Service)
			if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
				st.Fatalf("Failed waiting for Service %q to transition to Ready == True: %#v", names.Service, err)
			}

			st.Logf("Validating Route %q has cluster-local address", serviceresourcenames.Route(svc))
			// Check Route is not ready

			if err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, serviceresourcenames.Route(svc), hasPrivateRoute, "RouteIsClusterLocal"); err != nil {
				st.Fatalf("The Route %q should be privately visible but it was not: %#v", serviceresourcenames.Route(svc), err)
			}
		})
	}
}
