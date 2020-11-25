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

package ha

import (
	"context"
	"net/url"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	// NumControllerReconcilers is the number of controllers run by ./cmd/controller/main.go.
	// It is exported so the tests from cmd/controller/main.go can ensure we keep it in sync.
	NumControllerReconcilers = 7
)

func createPizzaPlanetService(t *testing.T, fopt ...rtesting.ServiceOption) (test.ResourceNames, *v1test.ResourceObjects) {
	t.Helper()
	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}
	resources, err := v1test.CreateServiceReady(t, clients, &names, fopt...)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}

	assertServiceEventuallyWorks(t, clients, names, resources.Service.Status.URL.URL(), test.PizzaPlanetText1)
	return names, resources
}

func assertServiceEventuallyWorks(t *testing.T, clients *test.Clients, names test.ResourceNames, url *url.URL, expectedText string) {
	t.Helper()
	// Wait for the Service to be ready.
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatal("Service not ready:", err)
	}
	// Wait for the Service to serve the expected text.
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(spoof.MatchesAllOf(spoof.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, url, expectedText, err)
	}
}

func waitForEndpointsState(client *pkgTest.KubeClient, svcName, svcNamespace string, inState func(*corev1.Endpoints) (bool, error)) error {
	endpointsService := client.CoreV1().Endpoints(svcNamespace)

	return wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		endpoint, err := endpointsService.Get(context.Background(), svcName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		return inState(endpoint)
	})
}

func readyEndpointsDoNotContain(ip string) func(*corev1.Endpoints) (bool, error) {
	return func(eps *corev1.Endpoints) (bool, error) {
		for _, subset := range eps.Subsets {
			for _, ready := range subset.Addresses {
				if ready.IP == ip {
					return false, nil
				}
			}
		}
		return true, nil
	}
}
