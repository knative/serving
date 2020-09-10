// +build e2e

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

package e2e

import (
	"net/http"
	"testing"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

//This test checks if the activator can probe
//the service when istio end user auth policy is
//applied on the service.
//This test needs istio side car injected and
//istio policy check enabled.
//policy is present in test/config/security/policy.yaml
//apply policy before running this test
func TestAllowedProbes(t *testing.T) {
	t.Parallel()

	clients := SetupServingNamespaceforSecurityTesting(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")
	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsOneOfStatusCodes(http.StatusUnauthorized))),
		"HelloWorldServesAuthFailed",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
	); err != nil {
		// check if side car is injected before reporting error
		if _, err := getContainer(clients.KubeClient, resources.Service.Name, "istio-proxy", resources.Service.Namespace); err != nil {
			t.Skip("Side car not enabled, skipping test")
		}
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected status %d: %v", url, names.Route, http.StatusUnauthorized, err)
	}
}
