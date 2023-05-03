package e2e

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/ptr"
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
	caCertDirectory    = "/var/lib/knative/ca"
	caCertPath         = caCertDirectory + "/ca.crt"
)

func TestProxyToHelloworld(t *testing.T, clients *test.Clients, helloworldURL *url.URL, inject bool, accessibleExternal bool) {
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

	caSecretName := os.Getenv("CA_CERT")

	// External services use different TLS certificates than cluster-local services.
	// Not passing CA_CERT will make the httpproxy use plain http to connect to the
	// target service.
	if caSecretName != "" && !accessibleExternal {
		envVars = append(envVars, []corev1.EnvVar{{Name: "CA_CERT", Value: caCertPath},
			{Name: "SERVER_NAME", Value: os.Getenv("SERVER_NAME")}}...)
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

	if caSecretName != "" && !accessibleExternal {
		serviceOptions = append(serviceOptions, rtesting.WithVolume("ca-certs", caCertDirectory, corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: caSecretName,
				Optional:   ptr.Bool(false),
			}}),
		)
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
