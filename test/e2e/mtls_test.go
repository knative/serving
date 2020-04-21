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
	"net/http"
	"os"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/networking"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/ingress"
	v1test "knative.dev/serving/test/v1"
)

func testSvcToKubeSvc(t *testing.T, clients *test.Clients, injectA, injectB bool, expStatus int) {
	t.Log("Creating a Pod and Service for the test app.")
	name := test.ObjectNameForTest(t)
	cleanup := createPodAndService(t, clients, name, injectA)
	defer cleanup()

	// Create envVars to be used in httpproxy app.
	envVars := []corev1.EnvVar{{
		Name:  targetHostEnv,
		Value: name + "." + test.ServingNamespace + ".svc",
	}}

	t.Log("Creating a Knative Service for the httpproxy test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "httpproxy",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithEnv(envVars...),
		rtesting.WithConfigAnnotations(map[string]string{
			"sidecar.istio.io/inject": strconv.FormatBool(injectB),
		}))
	if err != nil {
		t.Fatalf("Failed to create a Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsOneOfStatusCodes(expStatus))),
		"HTTPProxy",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
	); err != nil {
		t.Fatalf("Get unexpected error: %v", err)
	}

}

// In this test, we set up two apps: runtime and httpproxy.
// runtime is a raw k8s pod and svc, while httpproxy is a knative service.
// httpproxy is a proxy that redirects request to the runtime app.
// The expected result is that the request sent to httpproxy app is successfully redirected
// to runtime app when sidecar injection are both enabled or neither.
func TestSvcToKubeSvc(t *testing.T) {
	// testcases for table-driven testing.
	var testCases = []struct {
		name string
		// injectA indicates whether istio sidecar injection is enabled for runtime (k8s) service.
		// injectB indicates whether istio sidecar injection is enabled for httpproxy (knative) service.
		injectA   bool
		injectB   bool
		expStatus int
	}{
		{"both-disabled", false, false, http.StatusOK},
		{"a-disabled", false, true, http.StatusServiceUnavailable},
		{"b-disabled", true, false, http.StatusBadGateway},
		{"both-enabled", true, true, http.StatusOK},
	}

	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if os.Getenv("MESH") == "" && (tc.injectA || tc.injectB) {
				t.Skip("Skipping in no-mesh cluster")
			}
			cancel := logstream.Start(t)
			defer cancel()
			testSvcToKubeSvc(t, clients, tc.injectA, tc.injectB, tc.expStatus)
		})
	}
}

func createPodAndService(t *testing.T, clients *test.Clients, name string, inject bool) context.CancelFunc {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Labels: map[string]string{
				"test-pod": name,
			},
			Annotations: map[string]string{
				"sidecar.istio.io/inject": strconv.FormatBool(inject),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  name,
				Image: pkgTest.ImagePath("runtime"),
				Ports: []corev1.ContainerPort{{
					Name:          networking.ServicePortNameH2C,
					ContainerPort: 8080,
				}},
				// This is needed by the runtime image we are using.
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: strconv.Itoa(8080),
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(8080),
						},
					},
				},
			}},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Labels: map[string]string{
				"test-pod": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: "ClusterIP",
			Ports: []corev1.ServicePort{{
				Name:       networking.ServicePortNameH2C,
				Port:       int32(80),
				TargetPort: intstr.FromInt(int(8080)),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}
	return ingress.CreatePodAndService(t, clients, pod, svc)
}
