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
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	clients *test.Clients
	logger  *logging.BaseLogger
)

const (
	targetHostEnv      = "TARGET_HOST"
	helloworldResponse = "Hello World! How about some tasty noodles?"
)

func createTargetHostEnvVars(routeName string, t *testing.T) []corev1.EnvVar {
	helloWorldRoute, err := clients.ServingClient.Routes.Get(routeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Route of helloworld app: %v", err)
	}
	if helloWorldRoute.Status.Address == nil {
		t.Fatalf("Route is missing .Status.Address: %v", helloWorldRoute.Status)
	}
	internalDomain := helloWorldRoute.Status.Address.Hostname
	logger.Infof("helloworld internal domain is %v.", internalDomain)
	return []corev1.EnvVar{{
		Name:  targetHostEnv,
		Value: internalDomain,
	}}
}

func sendRequest(resolvableDomain bool, domain string) (*spoof.Response, error) {
	logger.Infof("The domain of request is %s.", domain)
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, resolvableDomain)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// In this test, we set up two apps: helloworld and httpproxy.
// helloworld is a simple app that displays a plaintext string.
// httpproxy is a proxy that redirects request to internal service of helloworld app
// with FQDN {route}.{namespace}.svc.cluster.local.
// The expected result is that the request sent to httpproxy app is successfully redirected
// to helloworld app.
func TestServiceToServiceCall(t *testing.T) {
	logger = logging.GetContextLogger("TestServiceToServiceCall")
	clients = Setup(t)

	// Set up helloworld app.
	helloWorldImagePath := test.ImagePath("helloworld")
	logger.Infof("Creating a Route and Configuration for helloworld test app.")
	helloWorldNames, err := CreateRouteAndConfig(clients, logger, helloWorldImagePath, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, helloWorldNames, logger) }, logger)
	defer TearDown(clients, helloWorldNames, logger)
	if err := test.WaitForRouteState(clients.ServingClient, helloWorldNames.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", helloWorldNames.Route, err)
	}

	// Set up httpproxy app.
	httpProxyImagePath := test.ImagePath("httpproxy")

	logger.Infof("Creating a Route and Configuration for httpproxy test app.")
	envVars := createTargetHostEnvVars(helloWorldNames.Route, t)
	httpProxyNames, err := CreateRouteAndConfig(clients, logger, httpProxyImagePath, &test.Options{
		EnvVars: envVars,
	})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, httpProxyNames, logger) }, logger)
	defer TearDown(clients, httpProxyNames, logger)
	if err := test.WaitForRouteState(clients.ServingClient, httpProxyNames.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", httpProxyNames.Route, err)
	}
	httpProxyRoute, err := clients.ServingClient.Routes.Get(httpProxyNames.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Route %s: %v", httpProxyNames.Route, err)
	}
	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		httpProxyRoute.Status.Domain, pkgTest.Retrying(pkgTest.MatchesAny, http.StatusServiceUnavailable, http.StatusNotFound),
		"HttpProxy",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Failed to start endpoint of httpproxy: %v", err)
	}
	logger.Info("httpproxy is ready.")

	// Send request to httpproxy to trigger the http call from httpproxy Pod to internal service of helloworld app.
	response, err := sendRequest(test.ServingFlags.ResolvableDomain, httpProxyRoute.Status.Domain)
	if err != nil {
		t.Fatalf("Failed to send request to httpproxy: %v", err)
	}
	// We expect the response from httpproxy is equal to the response from htlloworld
	if helloworldResponse != strings.TrimSpace(string(response.Body)) {
		t.Fatalf("The httpproxy response '%s' is not equal to helloworld response '%s'.", string(response.Body), helloworldResponse)
	}
}
