// +build e2e istio

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

	corev1 "k8s.io/api/core/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const targetHostEnv = "TARGET_HOST"

// In this test, the access to cluster-local-gateway is forbidden
// by istio authorizationpolicy.
//
// This test needs istio cluster gateway and authorizationpolicy
// to deny access from serving-tests-security ns to cluster-local-gateway.
func TestClusterLocalAuthorization(t *testing.T) {
	t.Parallel()

	clients := e2e.SetupServingNamespaceforSecurityTesting(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	withInternalVisibility := rtesting.WithServiceLabel(
		serving.VisibilityLabelKey, serving.VisibilityClusterLocal)
	resources, err := v1test.CreateServiceReady(t, clients, &names,
		withInternalVisibility,
		rtesting.WithConfigAnnotations(map[string]string{
			"sidecar.istio.io/inject": "false", // Do not enable injection otherwise get authentication error.
		}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if resources.Route.Status.URL.Host == "" {
		t.Fatalf("Route is missing .Status.URL: %#v", resources.Route.Status)
	}
	if resources.Route.Status.Address == nil {
		t.Fatalf("Route is missing .Status.Address: %#v", resources.Route.Status)
	}
	// Check that the target Route's Domain matches its cluster local address.
	if want, got := resources.Route.Status.Address.URL, resources.Route.Status.URL; got.String() != want.String() {
		t.Errorf("Route.Status.URL.Host = %v, want %v", got, want)
	}
	t.Logf("helloworld internal domain is %s.", resources.Route.Status.URL.Host)

	// Create envVars to be used in httpproxy app.
	envVars := []corev1.EnvVar{{
		Name:  targetHostEnv,
		Value: resources.Route.Status.URL.URL().Hostname(),
	}}

	// Set up httpproxy app.
	t.Log("Creating a Service for the httpproxy test app.")
	names = test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "httpproxy",
	}

	test.EnsureTearDown(t, clients, &names)

	resources, err = v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithEnv(envVars...),
		rtesting.WithConfigAnnotations(map[string]string{
			"sidecar.istio.io/inject": "false", // Do not enable injection otherwise get authentication error.
		}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsOneOfStatusCodes(http.StatusForbidden))),
		"HTTPProxy",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
	); err != nil {
		t.Fatal("Failed to start endpoint of httpproxy:", err)
	}
}
