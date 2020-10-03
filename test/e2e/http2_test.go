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
	"context"
	"testing"

	pkgTest "knative.dev/pkg/test"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestHelloHttp2WithPortNameH2C validates that an http/2-only service can be
// reached if the portName is "h2c".
func TestHelloHttp2WithPortNameH2C(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	// hellohttp2 returns client errors (4xx) if contacted via http1.1,
	// and behaves like helloworld if called with http/2.
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "hellohttp2",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, rtesting.WithNamedPort("h2c"))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.HelloHTTP2Text))),
		"HelloHttp2ServesTextOnH2C",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloHTTP2Text, err)
	}
}

// TestHelloHttp2WithEmptyPortName validates that an http/2-only service
// is unreachable if the port name is not specified.
// TODO(knative/serving#4283): Once the feature is implemented, this test
// should succeed.
func TestHelloHttp2WithEmptyPortName(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	// hellohttp2 returns client errors (4xx) if contacted via http1.1,
	// and behaves like helloworld if called with http/2.
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "hellohttp2",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, rtesting.WithNamedPort(""))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		pkgTest.IsOneOfStatusCodes(426),
		"HelloHttp2ServesTextWithEmptyPort",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected status code 426: %v", url, names.Route, err)
	}
}
