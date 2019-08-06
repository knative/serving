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
	"knative.dev/serving/test/e2e"
	"testing"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptest "knative.dev/pkg/test"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	v1b1testing "knative.dev/serving/pkg/testing/v1beta1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
	v1b1test "knative.dev/serving/test/v1beta1"
)

// These service names need to be stable, since we use them across
// multiple "go test" invocations.
const (
	ServiceName            = "pizzaplanet-upgrade-service"
	ScaleToZeroServiceName = "scale-to-zero-upgrade-service"
)

// Shamelessly cribbed from conformance/service_test.
func assertServiceResourcesUpdated(t *testing.T, clients *test.Clients, names test.ResourceNames, routeDomain, expectedText string) {
	t.Helper()
	// TODO(#1178): Remove "Wait" from all checks below this point.
	_, err := ptest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		routeDomain,
		v1a1test.RetryingRouteInconsistency(ptest.MatchesAllOf(ptest.IsStatusOK, ptest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, routeDomain, expectedText, err)
	}
}

//CreateService creates a new service and test if it is created successfully using a given
//version of client
func CreateService(t *testing.T, apiVersion string) {
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = ServiceName
	names.Image = test.PizzaPlanet1
	var domain string
	switch apiVersion {
	case "v1alpha1":
		resources, err := v1a1test.CreateRunLatestServiceLegacyReady(t, clients, &names)
		if err != nil {
			t.Fatalf("Failed to create Service: %v", err)
		}
		domain = resources.Service.Status.URL.Host
	case "v1beta1":
		resources, err := v1b1test.CreateServiceReady(t, clients, &names)
		if err != nil {
			t.Fatalf("Failed to create Service: %v", err)
		}
		domain = resources.Service.Status.URL.Host
	default:
		t.Fatal("Unknown API version")
	}

	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)
}

//CreateServiceAndScaleToZero creates a new service and scales it down to zero for upgrade testing using a given
//version of client
func CreateServiceAndScaleToZero(t *testing.T, apiVersion string) {
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = ScaleToZeroServiceName
	names.Image = test.PizzaPlanet1
	var domain string
	var revision string
	switch apiVersion {
	case "v1alpha1":
		resources, err := v1a1test.CreateRunLatestServiceLegacyReady(t, clients, &names)
		if err != nil {
			t.Fatalf("Failed to create Service: %v", err)
		}
		domain = resources.Service.Status.URL.Host
		revision = revisionresourcenames.Deployment(resources.Revision)
	case "v1beta1":
		resources, err := v1b1test.CreateServiceReady(t, clients, &names)
		if err != nil {
			t.Fatalf("Failed to create Service: %v", err)
		}
		domain = resources.Service.Status.URL.Host
		revision = revisionresourcenames.Deployment(resources.Revision)
	default:
		t.Fatal("Unknown API version")
	}
	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)

	if err := e2e.WaitForScaleToZero(t, revision, clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}

//UpdateService patches an existing service with a new image and check if it updated using a given version of client
func UpdateService(serviceName string, t *testing.T, apiVersion string) {
	t.Helper()
	clients := e2e.Setup(t)
	var names test.ResourceNames
	names.Service = serviceName
	var routeDomain string
	t.Logf("Getting service %q", names.Service)
	switch apiVersion {
	case "v1alpha1":
		svc, err := clients.ServingAlphaClient.Services.Get(names.Service, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Service: %v", err)
		}
		names.Route = serviceresourcenames.Route(svc)
		names.Config = serviceresourcenames.Configuration(svc)
		names.Revision = svc.Status.LatestCreatedRevisionName

		routeDomain = svc.Status.URL.Host
		t.Log("Check that we can hit the old service and get the old response.")
		assertServiceResourcesUpdated(t, clients, names, routeDomain, test.PizzaPlanetText1)

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
	case "v1beta1":
		svc, err := clients.ServingBetaClient.Services.Get(names.Service, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Service: %v", err)
		}
		names.Route = serviceresourcenames.Route(svc)
		names.Config = serviceresourcenames.Configuration(svc)
		names.Revision = svc.Status.LatestCreatedRevisionName

		routeDomain = svc.Status.URL.Host
		t.Log("Check that we can hit the old service and get the old response.")
		assertServiceResourcesUpdated(t, clients, names, routeDomain, test.PizzaPlanetText1)

		t.Log("Updating the Service to use a different image")
		newImage := ptest.ImagePath(test.PizzaPlanet2)
		if _, err := v1b1test.PatchService(t, clients, svc, v1b1testing.WithServiceImage(newImage)); err != nil {
			t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, newImage, err)
		}

		t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
		revisionName, err := v1b1test.WaitForServiceLatestRevision(clients, names)
		if err != nil {
			t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, test.PizzaPlanet2, err)
		}
		names.Revision = revisionName

		t.Log("When the Service reports as Ready, everything should be ready.")
		if err := v1b1test.WaitForServiceState(clients.ServingBetaClient, names.Service, v1b1test.IsServiceReady, "ServiceIsReady"); err != nil {
			t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
		}
	default:
		t.Fatal("Unknown API version")
	}
	assertServiceResourcesUpdated(t, clients, names, routeDomain, test.PizzaPlanetText2)
	defer test.TearDown(clients, names)
}
