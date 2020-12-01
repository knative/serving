/*
Copyright 2020 The Knative Authors

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
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptest "knative.dev/pkg/test"

	pkgupgrade "knative.dev/pkg/test/upgrade"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

// ServingPostDowngradeTests is an umbrella function for grouping all Serving post-downgrade tests.
func ServingPostDowngradeTests() []pkgupgrade.Operation {
	return []pkgupgrade.Operation{
		ServicePostDowngradeTest(),
		CreateNewServicePostDowngradeTest(),
	}
}

// ServicePostDowngradeTest verifies an existing service after downgrade.
func ServicePostDowngradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("ServicePostDowngradeTest", func(c pkgupgrade.Context) {
		servicePostDowngrade(c.T)
	})
}

func servicePostDowngrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: serviceName,
	}

	t.Logf("Getting service %q", names.Service)
	svc, err := clients.ServingClient.Services.Get(context.Background(), names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failed to get Service:", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)
	names.Revision = svc.Status.LatestCreatedRevisionName

	url := svc.Status.URL

	t.Log("Check that we can hit the old service and get the old response.")
	assertServiceResourcesUpdated(t, clients, names, url.URL(), test.PizzaPlanetText2)

	t.Log("Updating the Service to use a different image")
	newImage := ptest.ImagePath(test.PizzaPlanet1)
	if _, err := v1test.PatchService(t, clients, svc, rtesting.WithServiceImage(newImage)); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, newImage, err)
	}

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err := v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, test.PizzaPlanet1, err)
	}
	names.Revision = revisionName

	t.Log("When the Service reports as Ready, everything should be ready.")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, clients, names, url.URL(), test.PizzaPlanetText1)
}

// CreateNewServicePostDowngradeTest verifies creating a new service after downgrade.
func CreateNewServicePostDowngradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("CreateNewServicePostDowngradeTest", func(c pkgupgrade.Context) {
		createNewService(postDowngradeServiceName, c.T)
	})
}
