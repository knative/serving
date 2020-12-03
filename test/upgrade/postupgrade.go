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
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
	ptest "knative.dev/pkg/test"
	pkgupgrade "knative.dev/pkg/test/upgrade"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

func ServingPostUpgradeTests() []pkgupgrade.Operation {
	return []pkgupgrade.Operation{
		ServicePostUpgradeTest(),
		ServicePostUpgradeFromScaleToZeroTest(),
		BYORevisionPostUpgradeTest(),
		CreateNewServicePostUpgradeTest(),
		InitialScalePostUpgradeTest(),
	}
}

// ServicePostUpgradeTest verifies an existing service after upgrade.
func ServicePostUpgradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("ServicePostUpgradeTest", func(c pkgupgrade.Context) {
		servicePostUpgrade(c.T)
	})
}

func servicePostUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	// Before updating the service, the route and configuration objects should
	// not be updated just because there has been an upgrade.
	if hasGeneration, err := configHasGeneration(clients, serviceName, 1); err != nil {
		t.Fatal("Error comparing Configuration generation", err)
	} else if !hasGeneration {
		t.Fatal("Configuration is updated after an upgrade.")
	}
	if hasGeneration, err := routeHasGeneration(clients, serviceName, 1); err != nil {
		t.Fatal("Error comparing Route generation", err)
	} else if !hasGeneration {
		t.Fatal("Route is updated after an upgrade.")
	}

	updateService(serviceName, t)
}

func configHasGeneration(clients *test.Clients, serviceName string, generation int) (bool, error) {
	configObj, err := clients.ServingClient.Configs.Get(context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return configObj.Generation == int64(generation), nil
}

func routeHasGeneration(clients *test.Clients, serviceName string, generation int) (bool, error) {
	routeObj, err := clients.ServingClient.Routes.Get(context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return routeObj.Generation == int64(generation), nil
}

// ServicePostUpgradeFromScaleToZeroTest verifies a scaled-to-zero service after upgrade.
func ServicePostUpgradeFromScaleToZeroTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("PostUpgradeFromScaleToZeroTest", func(c pkgupgrade.Context) {
		updateService(scaleToZeroServiceName, c.T)
	})
}

// BYORevisionPostUpgradeTest attempts to update the RouteSpec of a Service using BYO Revision name. This
// test is meant to catch new defaults that break the immutability of BYO Revision name.
func BYORevisionPostUpgradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("BYORevisionPostUpgradeTest", func(c pkgupgrade.Context) {
		bYORevisionPostUpgrade(c.T)
	})
}

func bYORevisionPostUpgrade(t *testing.T) {
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
		t.Fatal("Failed to update Service:", err)
	}
}

func updateService(serviceName string, t *testing.T) {
	t.Helper()
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

	routeURL := svc.Status.URL.URL()

	t.Log("Check that we can hit the old service and get the old response.")
	assertServiceResourcesUpdated(t, clients, names, routeURL, test.PizzaPlanetText1)

	t.Log("Updating the Service to use a different image")
	newImage := ptest.ImagePath(test.PizzaPlanet2)
	if _, err := v1test.PatchService(t, clients, svc, rtesting.WithServiceImage(newImage)); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, newImage, err)
	}

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err := v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, test.PizzaPlanet2, err)
	}
	names.Revision = revisionName

	t.Log("When the Service reports as Ready, everything should be ready.")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, clients, names, routeURL, test.PizzaPlanetText2)
}

// CreateNewServicePostUpgradeTest verifies creating a new service after upgrade.
func CreateNewServicePostUpgradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("CreateNewServicePostUpgradeTest", func(c pkgupgrade.Context) {
		createNewService(postUpgradeServiceName, c.T)
	})
}

// InitialScalePostUpgradeTest verifies that the service is ready after upgrade
// despite the fact that it does not receive any requests.
func InitialScalePostUpgradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("InitialScalePostUpgradeTest", func(c pkgupgrade.Context) {
		initialScalePostUpgrade(c.T)
	})
}

func initialScalePostUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	t.Logf("Getting service %q", initialScaleServiceName)
	svc, err := clients.ServingClient.Services.Get(context.Background(), initialScaleServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failed to get Service:", err)
	}
	if !svc.IsReady() {
		t.Fatalf("Post upgrade Service is not ready with reason %q", svc.Status.GetCondition(v1.ServiceConditionRoutesReady).Reason)
	}
}
