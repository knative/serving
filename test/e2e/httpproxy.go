//go:build e2e
// +build e2e

/*
Copyright 2023 The Knative Authors

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
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/networking/pkg/certificates"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/ingress"
	"knative.dev/pkg/test/spoof"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	targetHostEnv      = "TARGET_HOST"
	gatewayHostEnv     = "GATEWAY_HOST"
	helloworldResponse = "Hello World! How about some tasty noodles?"
)

func TestProxyToHelloworld(t *testing.T, clients *test.Clients, helloworldURL *url.URL, inject bool, accessibleExternal bool, caSecret *corev1.Secret) {
	// Create envVars to be used in httpproxy app.
	envVars := []corev1.EnvVar{{
		Name:  targetHostEnv,
		Value: helloworldURL.Hostname(),
	}}

	// When resolvable domain is not set for external access test, use gateway for the endpoint as services like sslip.io may be flaky.
	// ref: https://github.com/knative/serving/issues/5389
	if !test.ServingFlags.ResolvableDomain && accessibleExternal {
		gatewayTarget, mapper, err := ingress.GetIngressEndpoint(context.Background(), clients.KubeClient, pkgTest.Flags.IngressEndpoint)
		if err != nil {
			t.Fatal("Failed to get gateway IP:", err)
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  gatewayHostEnv,
			Value: net.JoinHostPort(gatewayTarget, mapper("80")),
		})
	}

	// HTTPProxy needs to trust the CA that signed the cluster-local-domain certificates of the target service
	if caSecret != nil && !accessibleExternal {
		envVars = append(envVars, []corev1.EnvVar{{Name: "CA_CERT", Value: string(caSecret.Data[certificates.CertName])}}...)
	}

	// Set up httpproxy app.
	t.Log("Creating a Service for the httpproxy test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HTTPProxy,
	}

	test.EnsureTearDown(t, clients, &names)

	serviceOptions := []rtesting.ServiceOption{
		rtesting.WithEnv(envVars...),
		rtesting.WithConfigAnnotations(map[string]string{
			"sidecar.istio.io/inject": strconv.FormatBool(inject),
		}),
	}

	resources, err := v1test.CreateServiceReady(t, clients, &names, serviceOptions...)

	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err = pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(helloworldResponse)),
		"HTTPProxy",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatal("Failed to start endpoint of httpproxy:", err)
	}
	t.Log("httpproxy is ready.")

	// When we're testing with resolvable domains, we fail earlier trying
	// to resolve the cluster local domain.
	if !accessibleExternal && test.ServingFlags.ResolvableDomain {
		return
	}

	// if the service is not externally available,
	// the gateway does not expose a https port, so we need to call the http port
	if !accessibleExternal && helloworldURL.Scheme == "https" {
		helloworldURL.Scheme = "http"
	}

	// As a final check (since we know they are both up), check that if we can
	// (or cannot) access the helloworld app externally.
	response, err := sendRequest(t, clients, test.ServingFlags.ResolvableDomain, helloworldURL)
	if err != nil {
		t.Fatal("Unexpected error when sending request to helloworld:", err)
	}
	expectedStatus := http.StatusNotFound
	if accessibleExternal {
		expectedStatus = http.StatusOK
	}
	if got, want := response.StatusCode, expectedStatus; got != want {
		t.Errorf("helloworld response StatusCode = %v, want %v", got, want)
	}
}

func sendRequest(t *testing.T, clients *test.Clients, resolvableDomain bool, url *url.URL) (*spoof.Response, error) {
	t.Logf("The domain of request is %s.", url.Hostname())
	client, err := pkgTest.NewSpoofingClient(context.Background(), clients.KubeClient, t.Logf, url.Hostname(), resolvableDomain, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
