package e2e

import (
	"net/http"
	"testing"

	"github.com/knative/serving/test"

	corev1 "k8s.io/api/core/v1"
)

const (
	targetHostEnvName = "TARGET_HOST"
	targetHostDomain  = "www.google.com"
)

func TestEgressTraffic(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "httpproxy",
	}
	envVars := []corev1.EnvVar{{
		Name:  targetHostEnvName,
		Value: targetHostDomain,
	}}
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	service, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{EnvVars: envVars})
	if err != nil {
		t.Fatalf("Failed to create a service: %v", err)
	}
	response, err := sendRequest(t, clients, test.ServingFlags.ResolvableDomain, service.Route.Status.Domain)
	if err != nil {
		t.Fatalf("Failed to send request to httpproxy: %v", err)
	}
	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Fatalf("%v response StatusCode = %v, want %v", targetHostDomain, got, want)
	}
}
