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
	"testing"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

//This test checks if the activator can probe
//the service when istio end user auth policy is
//applied on the service.
//This test needs istio side car injected and
//istio policy check enabled.
//policy is present test/config/security/policy.yaml
//apply policy before running this test
func TestProbeWhitelist(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := SetupServingNamespaceforSecurityTesting(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	resources, httpsTransportOption, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, test.ServingFlags.Https)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	var opt interface{}
	if test.ServingFlags.Https {
		url.Scheme = "https"
		if httpsTransportOption == nil {
			t.Fatalf("Https transport option is nil")
		}
		opt = *httpsTransportOption
	}
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.MatchesBody(test.UnauthorizedText))),
		"HelloWorldServesAuthFailed",
		test.ServingFlags.ResolvableDomain,
		opt); err != nil {
		// check if side car is injected before reporting error
		if _, err := getContainer(clients.KubeClient, resources.Service.Name, "istio-proxy", resources.Service.Namespace); err != nil {
			t.Log("side car not enabled, skipping test")
			return
		}
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.UnauthorizedText, err)
	}
}
