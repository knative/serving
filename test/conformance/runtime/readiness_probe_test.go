// +build e2e

/*
Copyright 2019 The Knative Authors

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

package runtime

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"
)

func TestProbeRuntime(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	var testCases = []struct {
		// name of the test case, which will be inserted in names of routes, configurations, etc.
		// Use a short name here to avoid hitting the 63-character limit in names
		// (e.g., "service-to-service-call-svc-cluster-local-uagkdshh-frkml-service" is too long.)
		name    string
		handler corev1.Handler
		env     []corev1.EnvVar
	}{{
		name: "httpGet",
		env: []corev1.EnvVar{{
			Name:  "STARTUP_DELAY",
			Value: "10s",
		}},
		handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/healthz",
			},
		},
	}, {
		name: "tcpSocket",
		handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{},
		},
	}, {
		name: "exec",
		handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{"/ko-app/readiness", "probe"},
			},
		},
	}}

	for _, tc := range testCases {
		tc := tc
		for _, period := range []int32{0, 1} {
			period := period
			name := tc.name
			if period > 0 {
				// period > 0 opts out of the custom knative startup probing behaviour.
				name = fmt.Sprintf("%s-period=%d", name, period)
			}

			t.Run(name, func(t *testing.T) {
				t.Parallel()
				names := test.ResourceNames{
					Service: test.ObjectNameForTest(t),
					Image:   test.Readiness,
				}

				test.EnsureTearDown(t, clients, &names)

				t.Log("Creating a new Service")
				resources, err := v1test.CreateServiceReady(t, clients, &names,
					v1opts.WithEnv(tc.env...),
					v1opts.WithReadinessProbe(
						&corev1.Probe{
							Handler:       tc.handler,
							PeriodSeconds: period,
						}))
				if err != nil {
					t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
				}

				// Once the service reports ready we should immediately be able to curl it.
				url := resources.Route.Status.URL.URL()
				url.Path = "/healthz"
				if _, err = pkgtest.WaitForEndpointState(
					context.Background(),
					clients.KubeClient,
					t.Logf,
					url,
					v1test.RetryingRouteInconsistency(spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText))),
					"readinessIsReady",
					test.ServingFlags.ResolvableDomain,
					test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
				); err != nil {
					t.Fatalf("The endpoint for Route %s at %s didn't return success: %v", names.Route, url, err)
				}

				// Check if scaling down works even if access from liveness probe exists.
				if err := shared.WaitForScaleToZero(t, revisionresourcenames.Deployment(resources.Revision), clients); err != nil {
					t.Fatal("Could not scale to zero:", err)
				}
			})
		}
	}
}
