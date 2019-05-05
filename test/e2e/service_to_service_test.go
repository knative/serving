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
	"fmt"
	"net/http"
	"strings"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"

	corev1 "k8s.io/api/core/v1"

	"github.com/knative/serving/pkg/reconciler/revision/resources/names"
	routeconfig "github.com/knative/serving/pkg/reconciler/route/config"
	. "github.com/knative/serving/pkg/reconciler/testing"
)

const (
	targetHostEnv      = "TARGET_HOST"
	helloworldResponse = "Hello World! How about some tasty noodles?"
)

// testCases for table-driven testing.
var testCases = []struct {
	// name of the test case, which will be inserted in names of routes, configurations, etc.
	// Use a short name here to avoid hitting the 63-character limit in names
	// (e.g., "service-to-service-call-svc-cluster-local-uagkdshh-frkml-service" is too long.)
	name string
	// suffix to be trimmed from TARGET_HOST.
	suffix string
}{
	{"fqdn", ""},
	{"short", ".cluster.local"},
	{"shortest", ".svc.cluster.local"},
}

func sendRequest(t *testing.T, clients *test.Clients, resolvableDomain bool, domain string) (*spoof.Response, error) {
	t.Logf("The domain of request is %s.", domain)
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, resolvableDomain)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func testProxyToHelloworld(t *testing.T, clients *test.Clients, helloworldDomain string) {
	// Create envVars to be used in httpproxy app.
	envVars := []corev1.EnvVar{{
		Name:  targetHostEnv,
		Value: helloworldDomain,
	}}

	// Set up httpproxy app.
	t.Log("Creating a Service for the httpproxy test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "httpproxy",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)
	resources, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{
		EnvVars: envVars,
	})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	domain := resources.Route.Status.URL.Host
	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		pkgTest.Retrying(pkgTest.IsStatusOK, http.StatusNotFound),
		"HTTPProxy",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Failed to start endpoint of httpproxy: %v", err)
	}
	t.Log("httpproxy is ready.")

	// Send request to httpproxy to trigger the http call from httpproxy Pod to internal service of helloworld app.
	response, err := sendRequest(t, clients, test.ServingFlags.ResolvableDomain, domain)
	if err != nil {
		t.Fatalf("Failed to send request to httpproxy: %v", err)
	}
	// We expect the response from httpproxy is equal to the response from helloworld
	if helloworldResponse != strings.TrimSpace(string(response.Body)) {
		t.Fatalf("The httpproxy response '%s' is not equal to helloworld response '%s'.", string(response.Body), helloworldResponse)
	}

	// As a final check (since we know they are both up), check that we cannot send a request directly to the helloworld app.
	response, err = sendRequest(t, clients, test.ServingFlags.ResolvableDomain, helloworldDomain)
	if err != nil {
		if test.ServingFlags.ResolvableDomain {
			// When we're testing with resolvable domains, we might fail earlier trying
			// to resolve the shorter domain(s) off-cluster.
			return
		}
		t.Fatalf("Unexpected error when sending request to helloworld: %v", err)
	}

	if got, want := response.StatusCode, http.StatusNotFound; got != want {
		t.Errorf("helloworld response StatusCode = %v, want %v", got, want)
	}
}

// In this test, we set up two apps: helloworld and httpproxy.
// helloworld is a simple app that displays a plaintext string.
// httpproxy is a proxy that redirects request to internal service of helloworld app
// with FQDN {route}.{namespace}.svc.cluster.local, or {route}.{namespace}.svc, or
// {route}.{namespace}.
// The expected result is that the request sent to httpproxy app is successfully redirected
// to helloworld app.
func TestServiceToServiceCall(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	withInternalVisibility := WithServiceLabel(
		routeconfig.VisibilityLabelKey, routeconfig.VisibilityClusterLocal)
	resources, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{}, withInternalVisibility)
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
	if want, got := resources.Route.Status.Address.Hostname, resources.Route.Status.URL.Host; got != want {
		t.Errorf("Route.Status.URL.Host = %v, want %v", got, want)
	}
	t.Logf("helloworld internal domain is %s.", resources.Route.Status.URL.Host)

	// helloworld app and its route are ready. Running the test cases now.
	for _, tc := range testCases {
		helloworldDomain := strings.TrimSuffix(resources.Route.Status.URL.Host, tc.suffix)
		t.Run(tc.name, func(t *testing.T) {
			testProxyToHelloworld(t, clients, helloworldDomain)
		})
	}
}

// Same test as TestServiceToServiceCall but before sending requests
// we're waiting for target app to be scaled to zero
func TestServiceToServiceCallFromZero(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	t.Log("Creating helloworld Service")

	helloWorldNames := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	withInternalVisibility := WithServiceLabel(
		routeconfig.VisibilityLabelKey, routeconfig.VisibilityClusterLocal)

	test.CleanupOnInterrupt(func() { test.TearDown(clients, helloWorldNames) })
	defer test.TearDown(clients, helloWorldNames)

	helloWorld, err := test.CreateRunLatestServiceReady(t, clients, &helloWorldNames, &test.Options{}, withInternalVisibility)
	if err != nil {
		t.Fatalf("Failed to create a service: %v", err)
	}

	// Wait for service to be scaled to zero
	deploymentName := names.Deployment(helloWorld.Revision)
	if err := WaitForScaleToZero(t, deploymentName, clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}

	// Send request to helloworld app via httpproxy service
	testProxyToHelloworld(t, clients, helloWorld.Route.Status.URL.Host)
}
