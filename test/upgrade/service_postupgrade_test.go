// +build postupgrade

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

package upgrade

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
	ptest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

func TestRunLatestServicePostUpgrade(t *testing.T) {
	t.Parallel()
	updateService(serviceName, t)
}

func TestRunLatestServicePostUpgradeFromScaleToZero(t *testing.T) {
	t.Parallel()
	updateService(scaleToZeroServiceName, t)
}

// TestBYORevisionPostUpgrade attempts to update the RouteSpec of a Service using BYO Revision name. This
// test is meant to catch new defaults that break the immutability of BYO Revision name.
func TestBYORevisionPostUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: byoServiceName,
	}

	if _, err := v1test.UpdateServiceRouteSpec(t, clients, names, v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			Tag:          "example-tag",
			RevisionName: byoRevName,
			Percent:      ptr.Int64(100),
		}},
	}); err != nil {
		t.Fatalf("Failed to update Service: %v", err)
	}
}

func updateService(serviceName string, t *testing.T) {
	t.Helper()
	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: serviceName,
	}

	t.Logf("Getting service %q", names.Service)
	svc, err := clients.ServingAlphaClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)
	names.Revision = svc.Status.LatestCreatedRevisionName

	routeURL := svc.Status.URL.URL()

	t.Log("Check that we can hit the old service and get the old response.")
	assertServiceResourcesUpdated(t, clients, names, routeURL, test.PizzaPlanetText1)

	t.Log("Updating the Service to use a different image")
	newImage := ptest.ImagePath(test.PizzaPlanet2)
	if _, err := v1a1test.PatchServiceImage(t, clients, svc, newImage); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, newImage, err)
	}

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err := v1a1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, test.PizzaPlanet2, err)
	}
	names.Revision = revisionName

	t.Log("When the Service reports as Ready, everything should be ready.")
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, clients, names, routeURL, test.PizzaPlanetText2)
}
