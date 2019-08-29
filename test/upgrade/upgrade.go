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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/test/e2e"
	"knative.dev/serving/test/upgrade/dynamic"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	v1a1testing "knative.dev/serving/pkg/testing/v1alpha1"
	v1b1testing "knative.dev/serving/pkg/testing/v1beta1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
	v1b1test "knative.dev/serving/test/v1beta1"
)

//Test scenarios
var Tests = map[string]struct {
	clientType string
	testType   string
}{
	"Using v1alpha1_client": {clientType: "v1alpha1", testType: "v1alpha1test"},
	//TODO: Enable following test cases when the test cluster supports multiple CRD versions
	//"Using v1beta1_client":  {clientType: "v1beta1", testType: "v1beta1test"},
	//"Using v1alpha1 for pre upgrade and v1beta1 for post upgrade": {clientType: "v1alpha1", testType: "prea1postb1"},
	//"Using dynamic client": {clientType: "dynamic", testType: "dynamictest"},
}

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

//CreateServiceReady creates a new service and test if it is created successfully using v1alpha1 client
func CreateServiceUsingV1Alpha1Client(t *testing.T, serviceName string) {
	t.Helper()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = test.PizzaPlanet1
	resources, err := v1a1test.CreateRunLatestServiceLegacyReady(t, clients, &names,
		v1a1testing.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey: "1", //make sure we don't scale to zero during the test
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := resources.Service.Status.URL.Host
	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)
}

//CreateServiceReady creates a new service and test if it is created successfully using v1beta1 client
func CreateServiceUsingV1Beta1Client(t *testing.T, serviceName string) {
	t.Helper()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = test.PizzaPlanet1
	resources, err := v1b1test.CreateServiceReady(t, clients, &names,
		v1b1testing.WithServiceAnnotation(
			autoscaling.MinScaleAnnotationKey, "1", //make sure we don't scale to zero during the test
		),
	)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := resources.Service.Status.URL.Host
	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)
}

//CreateServiceAndScaleToZero creates a new service and scales it down to zero for upgrade testing using v1alpha1
//version of client
func CreateServiceAndScaleToZeroUsingV1Alpha1Client(t *testing.T, serviceName string) {
	t.Helper()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = test.PizzaPlanet1
	resources, err := v1a1test.CreateRunLatestServiceLegacyReady(t, clients, &names,
		v1a1testing.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey: autoscaling.WindowMin.String(), //make sure we scale to zero quickly
		}))
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := resources.Service.Status.URL.Host
	revision := revisionresourcenames.Deployment(resources.Revision)

	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)

	if err := e2e.WaitForScaleToZero(t, revision, clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}

//CreateServiceAndScaleToZero creates a new service and scales it down to zero for upgrade testing using v1beta1
//version of client
func CreateServiceAndScaleToZeroUsingV1Beta1Client(t *testing.T, serviceName string) {
	t.Helper()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = test.PizzaPlanet1

	resources, err := v1b1test.CreateServiceReady(t, clients, &names,
		v1b1testing.WithServiceAnnotation(autoscaling.WindowAnnotationKey, autoscaling.WindowMin.String()))
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := resources.Service.Status.URL.Host
	revision := revisionresourcenames.Deployment(resources.Revision)

	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)

	if err := e2e.WaitForScaleToZero(t, revision, clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}

//UpdateService patches an existing service with a new image and check if it updated
func UpdateService(t *testing.T, serviceName string, apiVersion string) {
	t.Helper()
	clients := e2e.Setup(t)
	var names test.ResourceNames
	names.Service = serviceName
	var routeDomain string

	defer test.TearDown(clients, names)

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
		newImage := pkgTest.ImagePath(test.PizzaPlanet2)
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
		newImage := pkgTest.ImagePath(test.PizzaPlanet2)
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

}

//GetServiceName generates uniques names for service according to given api version used to test
func GetServiceName(testType string) string {
	return "pizzaplanet-upgrade-service-" + testType
}

//GetScaleToZeroServiceName generates uniques names for service according to given api version used to test scale zero scenario
func GetScaleToZeroServiceName(testType string) string {
	return "scale-to-zero-upgrade-service-" + testType
}

//CreateServiceUsingDynamicClient creates a new service and test if it is created successfully using dynamic client
func CreateServiceUsingDynamicClient(t *testing.T, serviceName string) {
	t.Helper()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = test.PizzaPlanet1

	serviceConfig := v1a1test.LatestService(names, v1a1testing.WithConfigAnnotations(map[string]string{
		autoscaling.MinScaleAnnotationKey: "1", //make sure we don't scale to zero during the test
	}))
	serviceConfig.TypeMeta = metav1.TypeMeta{Kind: v1alpha1.Kind("Service").Kind,
		APIVersion: v1alpha1.SchemeGroupVersion.String()}

	uSvc := &unstructured.Unstructured{}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(serviceConfig)
	if err != nil {
		t.Fatalf("Failed to unmarshal as unstructured: %v: %v", names.Service, err)
	}
	uSvc.SetUnstructuredContent(u)
	resource, err := dynamic.CreateServiceReady(t, clients, uSvc)
	if err != nil {
		t.Fatalf("Failed to create service %s,%#v", names.Service, err)
	}

	assertServiceResourcesUpdated(t, clients, names, resource.Domain, test.PizzaPlanetText1)
}

//CreateServiceAndScaleToZeroUsingDynamicClient creates a new service and scales it down to zero for upgrade testing using dynamic client
func CreateServiceAndScaleToZeroUsingDynamicClient(t *testing.T, serviceName string) {
	t.Helper()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = test.PizzaPlanet1

	serviceConfig := v1a1test.LatestService(names, v1a1testing.WithConfigAnnotations(map[string]string{
		autoscaling.WindowAnnotationKey: autoscaling.WindowMin.String(), //make sure we scale to zero quickly
	}))
	serviceConfig.TypeMeta = metav1.TypeMeta{Kind: v1alpha1.Kind("Service").Kind,
		APIVersion: v1alpha1.SchemeGroupVersion.String()}

	uSvc := &unstructured.Unstructured{}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(serviceConfig)
	if err != nil {
		t.Fatalf("Failed to unmarshal as unstructured: %v: %v", names.Service, err)
	}
	uSvc.SetUnstructuredContent(u)
	resources, err := dynamic.CreateServiceReady(t, clients, uSvc)
	if err != nil {
		t.Fatalf("Failed to create service %s,%#v", names.Service, err)
	}

	assertServiceResourcesUpdated(t, clients, names, resources.Domain, test.PizzaPlanetText1)
	if err := e2e.WaitForScaleToZero(t, kmeta.ChildName(resources.Revision, "-deployment"), clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}

//UpdateServiceUsingDynamicClient patches an existing service with a new image and check if it updated using dynamic client
func UpdateServiceUsingDynamicClient(t *testing.T, serviceName string) {
	t.Helper()
	clients := e2e.Setup(t)
	var names test.ResourceNames
	names.Service = serviceName
	defer test.TearDown(clients, names)

	t.Logf("Getting service %q", names.Service)
	resources, err := dynamic.GetService(clients, serviceName)
	if err != nil {
		t.Fatalf("Failed to get service %s,%#v", names.Service, err)
	}
	t.Log("Check that we can hit the old service and get the old response.")
	assertServiceResourcesUpdated(t, clients, names, resources.Domain, test.PizzaPlanetText1)

	t.Log("Updating the Service to use a different image")
	newImage := pkgTest.ImagePath(test.PizzaPlanet2)
	names, err = dynamic.PatchServiceImage(clients, resources.Service, newImage)
	if err != nil {
		t.Fatalf("Failed to patch image of service %s,%#v", names.Service, err)
	}

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err := dynamic.WaitForServiceLatestRevision(t, clients, names.Service, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, test.PizzaPlanet2, err)
	}
	names.Revision = revisionName

	t.Log("When the Service reports as Ready, everything should be ready.")
	if err = dynamic.WaitForServiceReady(t, clients, names.Service); err != nil {
		t.Fatalf("Unable to get service %s ready,%#v", names.Service, err)
	}

	assertServiceResourcesUpdated(t, clients, names, names.Domain, test.PizzaPlanetText2)
}
