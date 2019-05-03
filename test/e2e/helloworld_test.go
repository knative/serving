// +build e2e

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

package e2e

import (
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
)

func TestHelloWorld(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	resources, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	domain := resources.Route.Status.URL.Host

	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(helloWorldExpectedOutput))),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}

	revision := resources.Revision
	if val, ok := revision.Labels["serving.knative.dev/configuration"]; ok {
		if val != names.Config {
			t.Fatalf("Expect confguration name in revision label %q but got %q ", names.Config, val)
		}
	} else {
		t.Fatalf("Failed to get configuration name from Revision label")
	}
	if val, ok := revision.Labels["serving.knative.dev/service"]; ok {
		if val != names.Service {
			t.Fatalf("Expect Service name in revision label %q but got %q ", names.Service, val)
		}
	} else {
		t.Fatalf("Failed to get Service name from Revision label")
	}
}
