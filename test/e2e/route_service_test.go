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
	"testing"

	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
	"knative.dev/pkg/test/logstream"
	"knative.dev/pkg/apis"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	. "knative.dev/serving/pkg/testing/v1alpha1"
)

var serviceCondSet = apis.NewLivingConditionSet(
	v1alpha1.ServiceConditionConfigurationsReady,
	v1alpha1.ServiceConditionRoutesReady,
)

// TestRouteNotReady tests the scenario that when route's status is
// Ready == false, the service's RouteReady value should change from
// Unknown to False
func TestRouteNotReady(t *testing.T) {
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
				TrafficTarget: v1beta1.TrafficTarget{
					RevisionName: "foobar",  // Invalid revision name. This allows Revision creation to succeed and Route configuration to fail
					Percent:      100,
				},
			},
		},
	})

	t.Log("Creating a new Service with RouteReady == Unknown and route Ready == False")
	svc, err := v1a1test.CreateLatestService(t, clients, names, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service %q: %#v", names.Service, err)
	}

	t.Logf("Waiting for Service %q to transition to Ready == false.", names.Service)
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceNotReady, "ServiceIsNotReady"); err != nil {
		t.Fatalf("Failed waiting for Service %q to transition to Ready false: %#v", names.Service, err)
	}

	t.Logf("Waiting for Route %q to transition to Ready == false.", serviceresourcenames.Route(svc))
	// Check Route is not ready
	if err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, serviceresourcenames.Route(svc), v1a1test.IsRouteNotReady, "RouteIsNotReady"); err != nil {
		t.Fatalf("The Route %s was marked as Ready to serve traffic but it should not be: %#v", names.Route, err)
	}

	// Wait for RouteReady to become false
	t.Log("Wait for the service RouteReady to be false")
	if err = v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceNotRouteReady, "ServiceRouteReadyFalse"); err != nil {
		t.Fatalf("Service RouteReady is not set to false")
	}
}
