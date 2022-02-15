package e2e

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
	"testing"
)

func TestReadinessPort(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.ReadinessPort,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		v1opts.WithReadinessProbe(
			&corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Scheme: "http",
						Port:   intstr.FromInt(8077),
						Path:   "/",
					},
				},
			}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText)),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}

}
