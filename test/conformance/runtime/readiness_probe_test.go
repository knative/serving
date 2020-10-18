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
	"testing"

	corev1 "k8s.io/api/core/v1"
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
		name string
		// handler to be used for readiness probe in user container.
		handler corev1.Handler
	}{{
		"httpGet",
		corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/healthz",
			},
		},
	}, {
		"tcpSocket",
		corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{},
		},
	}, {
		"exec",
		corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{"/ko-app/runtime", "probe"},
			},
		},
	}}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Images:  []string{test.Runtime},
			}

			test.EnsureTearDown(t, clients, &names)

			t.Log("Creating a new Service")
			resources, err := v1test.CreateServiceReady(t, clients, &names,
				v1opts.WithReadinessProbe(
					&corev1.Probe{
						Handler: tc.handler,
					}))
			if err != nil {
				t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
			}
			// Check if scaling down works even if access from liveness probe exists.
			if err := shared.WaitForScaleToZero(t, revisionresourcenames.Deployment(resources.Revision), clients); err != nil {
				t.Fatal("Could not scale to zero:", err)
			}
		})
	}
}
