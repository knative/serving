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

package multicontainerprobing

import (
	"context"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

func TestMultiContainerReadiness(t *testing.T) {
	t.Parallel()

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.ServingContainer,
		Sidecars: []string{
			test.ServingContainer,
			test.ServingContainer,
			test.Readiness,
		},
	}

	containers := []corev1.Container{
		{
			Image: pkgTest.ImagePath(names.Image),
			Ports: []corev1.ContainerPort{{
				ContainerPort: 8881,
			}},
			Env: []corev1.EnvVar{
				// A port in the next container to forward requests to.
				{Name: "FORWARD_PORT", Value: "8882"},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt32(8881),
					}},
			},
		}, { // Sidecar with readiness probe.
			Image: pkgTest.ImagePath(names.Sidecars[0]),
			Env: []corev1.EnvVar{
				{Name: "PORT", Value: "8882"},
				{Name: "FORWARD_PORT", Value: "8883"},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt32(8882),
					}},
			},
		}, { // Sidecar with liveness probe.
			Image: pkgTest.ImagePath(names.Sidecars[1]),
			Env: []corev1.EnvVar{
				{Name: "PORT", Value: "8883"},
				{Name: "FORWARD_PORT", Value: "8884"},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt32(8883),
					}},
			},
		}, { // Sidecar with both readiness and liveness probes.
			Image: pkgTest.ImagePath(names.Sidecars[2]),
			Env: []corev1.EnvVar{
				{Name: "PORT", Value: "8884"},
				// Delay readiness. The Knative service should be ready only after all containers
				// are ready and the subsequent request should pass.
				{Name: "READY_DELAY", Value: "10s"},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.FromInt32(8884),
					}},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.FromInt32(8884),
					}},
			},
		},
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers = containers
	})
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
		"MulticontainerServesExpectedText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}
}

// TestMultiContainerReadinessDifferentProbeTypes check that sidecars can use different probe types.
func TestMultiContainerReadinessDifferentProbeTypes(t *testing.T) {
	t.Parallel()

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Readiness,
		Sidecars: []string{
			test.Readiness,
			test.Readiness,
			test.GRPCPing,
		},
	}

	containers := []corev1.Container{
		{
			Image: pkgTest.ImagePath(names.Image),
			Ports: []corev1.ContainerPort{{
				ContainerPort: 8080,
			}},
		}, { // Sidecar with TCPSocket startup, readiness and liveness probes.
			Image: pkgTest.ImagePath(names.Sidecars[0]),
			Env: []corev1.EnvVar{
				{Name: "PORT", Value: "8881"},
			},
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt32(8881),
					},
				},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt32(8881),
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt32(8881),
					},
				},
			},
		}, { // Sidecar with HTTPGet startup, HTTPGet readiness and Exec liveness probes.
			Image: pkgTest.ImagePath(names.Sidecars[1]),
			Env: []corev1.EnvVar{
				{Name: "PORT", Value: "8882"},
			},
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz/readiness",
						Port: intstr.FromInt32(8882),
					},
				},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz/readiness",
						Port: intstr.FromInt32(8882),
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/ko-app/readiness", "probe"},
					},
				},
			},
		}, { // Sidecar with GRPC startup, readiness and liveness probes.
			Image: pkgTest.ImagePath(names.Sidecars[2]),
			Env: []corev1.EnvVar{
				{Name: "PORT", Value: "8883"},
			},
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					GRPC: &corev1.GRPCAction{
						Port: 8883,
					},
				},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					GRPC: &corev1.GRPCAction{
						Port: 8883,
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					GRPC: &corev1.GRPCAction{
						Port: 8883,
					},
				},
			},
		},
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers = containers
	})
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
		"MulticontainerServesExpectedText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}
}

func TestMultiContainerProbeStartFailingAfterReady(t *testing.T) {
	t.Parallel()

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Readiness,
		Sidecars: []string{
			test.Readiness,
		},
	}

	containers := []corev1.Container{
		{
			Image: pkgTest.ImagePath(names.Image),
			Ports: []corev1.ContainerPort{{
				ContainerPort: 8080,
			}},
			Env: []corev1.EnvVar{
				// The port of the sidecar where a request to start failing readiness should be sent.
				{Name: "FORWARD_PORT", Value: "8881"},
			},
			ReadinessProbe: &corev1.Probe{
				// Ensure quicker propagation of the readiness failure.
				PeriodSeconds:    1,
				FailureThreshold: 3,
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz/readiness",
						Port: intstr.FromInt32(8080),
					}},
			},
		}, {
			Image: pkgTest.ImagePath(names.Sidecars[0]),
			Env: []corev1.EnvVar{
				{Name: "PORT", Value: "8881"},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz/readiness",
						Port: intstr.FromInt32(8881),
					}},
			},
		},
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	objects, err := v1test.CreateServiceReady(t, clients, &names, func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers = containers
	}, rtesting.WithConfigAnnotations(map[string]string{
		// Make sure we don't scale to zero during the test.
		autoscaling.MinScaleAnnotationKey: "1",
	}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := objects.Route.Status.URL.URL()
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText)),
		"MulticontainerServesExpectedText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}

	t.Log("POST to /start-failing-sidecar")
	client, err := pkgTest.NewSpoofingClient(context.Background(),
		clients.KubeClient,
		t.Logf,
		url.Hostname(),
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		t.Fatalf("Failed to create spoofing client: %v", err)
	}

	url.Path = "/start-failing-sidecar"
	startFailing, err := http.NewRequest(http.MethodPost, url.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(startFailing); err != nil {
		t.Fatalf("POST to /start-failing-sidecar failed: %v", err)
	}

	revName, err := e2e.RevisionFromConfiguration(clients, names.Service)
	if err != nil {
		t.Fatal("Unable to get revision: ", err)
	}
	privateSvcName := e2e.PrivateServiceName(t, clients, revName)
	endpoints := clients.KubeClient.CoreV1().Endpoints(test.ServingFlags.TestNamespace)

	var latestReady int
	if err := wait.PollUntilContextTimeout(context.Background(), time.Second, 60*time.Second, true, func(context.Context) (bool, error) {
		endpoint, err := endpoints.Get(context.Background(), privateSvcName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		latestReady = resources.ReadyAddressCount(endpoint)
		return latestReady == 0, nil
	}); err != nil {
		t.Fatalf("Service still has endpoints: %d", latestReady)
	}
}
