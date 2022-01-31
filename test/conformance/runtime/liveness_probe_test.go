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

package runtime

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"
	"testing"
	"time"
)

const livenessPath = "/healthz/liveness"

func TestLiveness(t *testing.T) {
	t.Parallel()
	if test.ServingFlags.DisableOptionalAPI {
		t.Skip("Container.livenessProbe is not required by Knative Serving API Specification")
	}
	clients := test.Setup(t)

	var testCases = []struct {
		// name of the test case, which will be inserted in names of routes, configurations, etc.
		// Use a short name here to avoid hitting the 63-character limit in names
		// (e.g., "service-to-service-call-svc-cluster-local-uagkdshh-frkml-service" is too long.)
		name    string
		handler corev1.Handler
		sleep   bool
	}{{
		name: "httpGet",
		handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: livenessPath,
			},
		},
	}, {
		name: "httpGetAfterFirstProbe",
		handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: livenessPath,
			},
		},
		sleep: true,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.HealthProbes,
			}

			test.EnsureTearDown(t, clients, &names)

			t.Log("Creating a new Service")
			resources, err := v1test.CreateServiceReady(t, clients, &names,
				v1opts.WithLivenessProbe(
					&corev1.Probe{
						Handler:          tc.handler,
						PeriodSeconds:    10,
						FailureThreshold: 1,
					}))
			if err != nil {
				t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
			}

			// If true sleeping till the first kubelet probe check.
			if tc.sleep {
				time.Sleep(15 * time.Second)
			}
			url := resources.Route.Status.URL.URL()
			url.Path = livenessPath
			if _, err = pkgtest.CheckEndpointState(
				context.Background(),
				clients.KubeClient,
				t.Logf,
				url,
				spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.LivenessText+"15")),
				"livenessIsReady",
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

// commented out because, for this to work, https://github.com/knative/pkg/issues/2407 needs to be
// fixed, with another issue of a ksvc pod never properly restarting after going into a liveness
// probe fail.

/*
func TestLivenessWithFail(t *testing.T) {
	t.Parallel()
	if test.ServingFlags.DisableOptionalAPI {
		t.Skip("Container.livenessProbe is not required by Knative Serving API Specification")
	}
	clients := test.Setup(t)

	var testCases = []struct {
		// name of the test case, which will be inserted in names of routes, configurations, etc.
		// Use a short name here to avoid hitting the 63-character limit in names
		// (e.g., "service-to-service-call-svc-cluster-local-uagkdshh-frkml-service" is too long.)
		name    string
		handler corev1.Handler
		sleep	bool
	}{{
		name: "httpGet",
		handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: livenessPath,
			},
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.HealthProbes,
			}

			test.EnsureTearDown(t, clients, &names)

			t.Log("Creating a new Service")
			resources, err := v1test.CreateServiceReady(t, clients, &names,
				v1opts.WithLivenessProbe(
					&corev1.Probe{
						Handler: tc.handler,
						PeriodSeconds: 13,
						FailureThreshold: 1,
					}))
			if err != nil {
				t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
			}

			url := resources.Route.Status.URL.URL()
			url.Path = livenessPath
			if _, err = pkgtest.WaitForEndpointState(
				context.Background(),
				clients.KubeClient,
				t.Logf,
				url,
				spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.LivenessText + "15")),
				"livenessIsReady",
				test.ServingFlags.ResolvableDomain,
				test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
			); err != nil {
				t.Fatalf("The endpoint for Route %s at %s didn't return success: %v", names.Route, url, err)
			}

			t.Log("POST to /start-failing")
			client, err := pkgtest.NewSpoofingClient(context.Background(),
				clients.KubeClient,
				t.Logf,
				url.Hostname(),
				test.ServingFlags.ResolvableDomain,
				test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
			if err != nil {
				t.Fatalf("Failed to create spoofing client: %v", err)
			}

			url.Path = "/start-failing"
			startFailing, err := http.NewRequest(http.MethodPost, url.String(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Do(startFailing); err != nil {
				t.Fatalf("POST to /start-failing failed: %v", err)
			}

			url.Path = livenessPath
			if _, err = pkgtest.WaitForEndpointState(
				context.Background(),
				clients.KubeClient,
				t.Logf,
				url,
				spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.LivenessText + "15")),
				"livenessIsReady",
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

*/
