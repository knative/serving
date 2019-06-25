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

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgTest "knative.dev/pkg/test"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

const (
	// These service names need to be stable, since we use them across
	// multiple "go test" invocations.
	serviceName            = "pizzaplanet-upgrade-service"
	scaleToZeroServiceName = "scale-to-zero-upgrade-service"
)

// Shamelessly cribbed from conformance/service_test.
func assertServiceResourcesUpdated(t *testing.T, clients *test.Clients, names test.ResourceNames, routeDomain, expectedText string) {
	t.Helper()
	// TODO(#1178): Remove "Wait" from all checks below this point.
	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		routeDomain,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, routeDomain, expectedText, err)
	}
}

// PizzaPlanet1 -> PizzaPlanet2
func updateService(serviceName string, t *testing.T) (*test.Clients, string) {
	t.Helper()
	return updateImage(serviceName, t, test.PizzaPlanetText1, test.PizzaPlanet2, test.PizzaPlanetText2)
}

// PizzaPlanet2 -> PizzaPlanet1
func rollbackService(serviceName string, t *testing.T) (*test.Clients, string) {
	t.Helper()
	return updateImage(serviceName, t, test.PizzaPlanetText2, test.PizzaPlanet1, test.PizzaPlanetText1)

}

// Returns the new revision name so we can wait for it to scale to zero.
func updateImage(serviceName string, t *testing.T, oldResponse, image, newResponse string) (*test.Clients, string) {
	t.Helper()
	clients := e2e.Setup(t)
	var names test.ResourceNames
	names.Service = serviceName

	t.Logf("Getting service %q", names.Service)
	svc, err := clients.ServingAlphaClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)
	names.Revision = svc.Status.LatestCreatedRevisionName

	routeDomain := svc.Status.URL.Host

	t.Log("Check that we can hit the old service and get the old response.")
	assertServiceResourcesUpdated(t, clients, names, routeDomain, oldResponse)

	t.Log("Updating the Service to use a different image")
	newImage := pkgTest.ImagePath(image)
	if _, err := v1a1test.PatchServiceImage(t, clients, svc, newImage); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, newImage, err)
	}

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err := v1a1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, newImage, err)
	}
	names.Revision = revisionName

	t.Log("When the Service reports as Ready, everything should be ready.")
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, clients, names, routeDomain, newResponse)

	return clients, revisionName
}
