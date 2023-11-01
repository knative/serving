//go:build e2e
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
	"net/url"
	"strconv"
	"strings"
	"testing"

	netapi "knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
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

// testcases for table-driven testing.
var testInjection = []struct {
	name string
	// injectA indicates whether istio sidecar injection is enabled for httpproxy service
	// injectB indicates whether istio sidecar injection is enabled for helloworld service
	injectA bool
	injectB bool
}{
	{"both-disabled", false, false},
	{"a-disabled", false, true},
	{"b-disabled", true, false},
	{"both-enabled", true, true},
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
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	withInternalVisibility := rtesting.WithServiceLabel(
		netapi.VisibilityLabelKey, serving.VisibilityClusterLocal)
	resources, err := v1test.CreateServiceReady(t, clients, &names, withInternalVisibility)
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

	// if cluster-local-domain-tls is enabled, this will return the CA used to sign the certificates.
	// TestProxyToHelloworld will use this CA to verify the https connection
	secret, err := GetCASecret(clients)
	if err != nil {
		t.Fatal(err.Error())
	}

	// helloworld app and its route are ready. Running the test cases now.
	for _, tc := range testCases {
		helloworldURL := &url.URL{
			Scheme: resources.Route.Status.URL.Scheme,
			Host:   strings.TrimSuffix(resources.Route.Status.URL.Host, tc.suffix),
			Path:   resources.Route.Status.URL.Path,
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !test.ServingFlags.DisableLogStream {
				cancel := logstream.Start(t)
				defer cancel()
			}
			TestProxyToHelloworld(t, clients, helloworldURL, true, false, secret)
		})
	}
}

func testSvcToSvcCallViaActivator(t *testing.T, clients *test.Clients, injectA bool, injectB bool) {
	t.Log("Creating helloworld Service")

	testNames := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	withInternalVisibility := rtesting.WithServiceLabel(
		netapi.VisibilityLabelKey, serving.VisibilityClusterLocal)

	test.EnsureTearDown(t, clients, &testNames)

	resources, err := v1test.CreateServiceReady(t, clients, &testNames,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			"sidecar.istio.io/inject":          strconv.FormatBool(injectB),
		}), withInternalVisibility)
	if err != nil {
		t.Fatal("Failed to create a service:", err)
	}

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(&TestContext{
		t:         t,
		resources: resources,
		clients:   clients,
	}); err != nil {
		t.Fatal("Never got Activator endpoints in the service:", err)
	}

	// if cluster-local-domain-tls is enabled, this will return the CA used to sign the certificates.
	// TestProxyToHelloworld will use this CA to verify the https connection
	secret, err := GetCASecret(clients)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Send request to helloworld app via httpproxy service
	TestProxyToHelloworld(t, clients, resources.Route.Status.URL.URL(), injectA, false, secret)
}

// Same test as TestServiceToServiceCall but before sending requests
// we're waiting for target app to be scaled to zero
func TestSvcToSvcViaActivator(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	for _, tc := range testInjection {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !test.ServingFlags.DisableLogStream {
				cancel := logstream.Start(t)
				defer cancel()
			}
			testSvcToSvcCallViaActivator(t, clients, tc.injectA, tc.injectB)
		})
	}
}

// This test is similar to TestServiceToServiceCall, but creates an external accessible helloworld service instead.
// It verifies that the helloworld service is accessible internally from both internal domain and external domain.
// But it's only accessible from external via the external domain
func TestCallToPublicService(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if resources.Route.Status.URL.Host == "" {
		t.Fatalf("Route is missing .Status.URL: %#v", resources.Route.Status)
	}
	if resources.Route.Status.Address == nil {
		t.Fatalf("Route is missing .Status.Address: %#v", resources.Route.Status)
	}

	gatewayTestCases := []struct {
		name                 string
		url                  *url.URL
		accessibleExternally bool
	}{
		{"local_address", resources.Route.Status.Address.URL.URL(), false},
		{"external_address", resources.Route.Status.URL.URL(), true},
	}

	// if cluster-local-domain-tls is enabled, this will return the CA used to sign the certificates.
	// TestProxyToHelloworld will use this CA to verify the https connection
	secret, err := GetCASecret(clients)
	if err != nil {
		t.Fatal(err.Error())
	}

	for _, tc := range gatewayTestCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !test.ServingFlags.DisableLogStream {
				cancel := logstream.Start(t)
				defer cancel()
			}
			TestProxyToHelloworld(t, clients, tc.url, false, tc.accessibleExternally, secret)
		})
	}
}
