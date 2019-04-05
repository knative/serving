package e2e

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/knative/serving/test"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	targetHostDomain = "httpbin.org"
	testString       = "Would you like a cup of coffee?"
)

func TestEgressTraffic(t *testing.T) {
	t.Parallel()
	clients := Setup(t)
	t.Log("Creating a Route and Configuration for httpproxy test app.")

	svcName := test.ObjectNameForTest(t)
	httpProxyNames := test.ResourceNames{
		Config: svcName,
		Route:  svcName,
		Image:  "httpproxy",
	}

	envVars := []corev1.EnvVar{{
		Name:  targetHostEnv,
		Value: targetHostDomain,
	}}
	// Set up httpproxy app
	t.Log("Creating a Route and Configuration for httpproxy test app.")
	httpProxyNames, err := CreateRouteAndConfig(t, clients, "httpproxy", &test.Options{
		EnvVars: envVars,
	})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, httpProxyNames) })
	defer test.TearDown(clients, httpProxyNames)
	if err := test.WaitForRouteState(clients.ServingClient, httpProxyNames.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", httpProxyNames.Route, err)
	}
	httpProxyRoute, err := clients.ServingClient.Routes.Get(httpProxyNames.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Route %s: %v", httpProxyNames.Route, err)
	}

	// Send base64 encoded string to decode endpoint to make sure that we're talking to httpbin service
	b64DecodeURL := httpProxyRoute.Status.Domain + "/base64/" + base64.StdEncoding.EncodeToString([]byte(testString))
	response, err := sendRequest(t, clients, test.ServingFlags.ResolvableDomain, b64DecodeURL)
	if err != nil {
		t.Fatalf("Failed to send request to httpproxy: %v", err)
	}
	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Errorf("httpbin response StatusCode = %v, want %v", got, want)
	}
	if strings.TrimSpace(string(response.Body)) != testString {
		t.Fatalf("The httpproxy response '%s' is not equal to test string '%s'.", string(response.Body), testString)
	}
}
