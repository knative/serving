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
	"fmt"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logstream"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func checkResponse(t *testing.T, clients *test.Clients, names test.ResourceNames, expectedText string) error {
	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		names.Domain,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		return fmt.Errorf("the endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, names.Domain, expectedText, err)
	}

	return nil
}

func TestMultipleNamespace(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	defaultClients := Setup(t) // This one uses the default namespace `test.ServingNamespace`
	altClients := SetupAlternativeNamespace(t)

	serviceName := test.ObjectNameForTest(t)

	defaultResources := test.ResourceNames{
		Service: serviceName,
		Image:   test.PizzaPlanet1,
	}
	test.CleanupOnInterrupt(func() { test.TearDown(defaultClients, defaultResources) })
	defer test.TearDown(defaultClients, defaultResources)
	if _, err := v1a1test.CreateRunLatestServiceReady(t, defaultClients, &defaultResources, &v1a1test.Options{}); err != nil {
		t.Fatalf("Failed to create Service %v in namespace %v: %v", defaultResources.Service, test.ServingNamespace, err)
	}

	altResources := test.ResourceNames{
		Service: serviceName,
		Image:   test.PizzaPlanet2,
	}
	test.CleanupOnInterrupt(func() { test.TearDown(altClients, altResources) })
	defer test.TearDown(altClients, altResources)
	if _, err := v1a1test.CreateRunLatestServiceReady(t, altClients, &altResources, &v1a1test.Options{}); err != nil {
		t.Fatalf("Failed to create Service %v in namespace %v: %v", altResources.Service, test.AlternativeServingNamespace, err)
	}

	if err := checkResponse(t, defaultClients, defaultResources, test.PizzaPlanetText1); err != nil {
		t.Error(err)
	}

	if err := checkResponse(t, altClients, altResources, test.PizzaPlanetText2); err != nil {
		t.Error(err)
	}
}

// This test is to ensure we do not leak deletion of services in other namespaces when deleting a route.
func TestConflictingRouteService(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	names := test.ResourceNames{
		Service:       test.AppendRandomString("conflicting-route-service"),
		TrafficTarget: "chips",
		Image:         test.PizzaPlanet1,
	}

	// Create a service in a different namespace but route label points to a route in another namespace
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      test.AppendRandomString("conflicting-route-service"),
			Namespace: test.AlternativeServingNamespace,
			Labels: map[string]string{
				serving.RouteLabelKey: names.Service,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "some-internal-addr",
		},
	}

	altClients := SetupAlternativeNamespace(t)
	altClients.KubeClient.Kube.CoreV1().Services(test.AlternativeServingNamespace).Create(svc)
	cleanup := func() {
		altClients.KubeClient.Kube.CoreV1().Services(test.AlternativeServingNamespace).Delete(svc.Name, &v1.DeleteOptions{})
	}
	test.CleanupOnInterrupt(cleanup)
	defer cleanup()

	clients := Setup(t)

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)
	if _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{}); err != nil {
		t.Errorf("Failed to create Service %v in namespace %v: %v", names.Service, test.ServingNamespace, err)
	}
}
