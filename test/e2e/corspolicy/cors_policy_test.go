//go:build e2e
// +build e2e

/*
Copyright 2024 The Knative Authors

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

package corspolicy

import (
	"context"
	"net/http"
	"testing"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	allowOriginHeaderKey  = "Access-Control-Allow-Origin"
	allowMethodsHeaderKey = "Access-Control-Allow-Methods"
)

func TestCorsPolicy(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")
	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create Service: %v: %v", names.Service, err)
	}

	t.Log("Wait for the service domains to be ready")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	serviceUrl := resources.Route.Status.URL.URL()
	t.Logf("The domain of request is %s.", serviceUrl.Hostname())
	client, err := pkgTest.NewSpoofingClient(context.Background(),
		clients.KubeClient,
		t.Logf,
		serviceUrl.Hostname(),
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		t.Fatalf("Failed to create spoofing client: %v", err)
	}

	request, err := http.NewRequest(http.MethodOptions, serviceUrl.String(), nil)
	request.Header.Add("Origin", "any-domain.com")
	request.Header.Add("Access-Control-Request-Method", "GET,OPTIONS")
	if err != nil {
		t.Fatalf("Failed to create a request: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Unexpected error when sending request to service: %v", err)
	}

	expectedStatus := http.StatusOK
	if got, want := response.StatusCode, expectedStatus; got != want {
		t.Errorf("Service response StatusCode = %v, want %v", got, want)
	}

	if respHeader := response.Header[allowOriginHeaderKey]; respHeader == nil {
		t.Errorf("CORS headers not found in response: %s", allowOriginHeaderKey)
	}
	if respHeader := response.Header[allowMethodsHeaderKey]; respHeader == nil {
		t.Errorf("CORS headers not found in response: %s", allowMethodsHeaderKey)
	}
}
