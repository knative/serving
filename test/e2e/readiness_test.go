//go:build e2e
// +build e2e

/*
Copyright 2022 The Knative Authors

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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	livenessPath  = "/healthz/liveness"
	readinessPath = "/healthz/readiness"
)

func TestReadinessAlternatePort(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Readiness,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		v1opts.WithEnv(corev1.EnvVar{
			Name:  "HEALTHCHECK_PORT",
			Value: "8077",
		}),
		v1opts.WithReadinessProbe(
			&corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Scheme: "http",
						Port:   intstr.FromInt(8077),
						Path:   "/readiness",
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

func TestReadinessPathAndQuery(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Readiness,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		v1opts.WithReadinessProbe(
			&corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/query?probe=ok",
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

func TestReadinessGRPCProbe(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.GRPCPing,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		v1opts.WithNamedPort("h2c"),
		v1opts.WithReadinessProbe(
			&corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					GRPC: &corev1.GRPCAction{
						Port: v1.DefaultUserPort,
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
		spoof.IsStatusOK,
		"gRPCProbeReady",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't return success: %v", url, names.Route, err)
	}
}

// TestLivenessProbeAwareOfStartupProbe verifies that liveness probes will only start after startup
// probes finished. Having a long startup probe shouldn't cause the Kubelet to restart the container
// too early due to a liveness check failing.
func TestLivenessProbeAwareOfStartupProbe(t *testing.T) {
	t.Parallel()

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Readiness,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")
	resources, err := v1test.CreateServiceReady(t, clients, &names,
		v1opts.WithEnv(corev1.EnvVar{
			Name:  "READY_DELAY",
			Value: "10s",
		}),
		v1opts.WithStartupProbe(
			&corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: readinessPath,
						Port: intstr.FromInt32(8080),
					},
				},
				PeriodSeconds: 1,
				// Must be longer than READY_DELAY otherwise Kubelet will restart the container.
				FailureThreshold: 20,
			}),
		v1opts.WithLivenessProbe(
			&corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: livenessPath,
						Port: intstr.FromInt32(8080),
					},
				},
				PeriodSeconds: 1,
				// Intentionally shorter than READY_DELAY.
				FailureThreshold: 3,
			}),
	)

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
		"containerServesExpectedText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}

	// Check that user-container hasn't been restarted.
	deploymentName := resourcenames.Deployment(resources.Revision)
	podList, err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal("Unable to get pod list: ", err)
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		if strings.Contains(pod.Name, deploymentName) && test.UserContainerRestarted(pod) {
			t.Fatal("User container unexpectedly restarted")
		}
	}
}
