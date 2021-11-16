//go:build e2e
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
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"
)

// readinessPropagationTime is how long to poll to allow for readiness probe
// changes to propagate to ingresses/activator.
// This is based on the default scaleToZeroGracePeriod.
const readinessPropagationTime = 30 * time.Second

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
			Name:  "READY_DELAY",
			Value: "10s",
		}},
		handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/healthz",
			},
		},
	}, {
		name: "tcpSocket",
		env: []corev1.EnvVar{{
			Name:  "LISTEN_DELAY",
			Value: "10s",
		}},
		handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{},
		},
	}, {
		name: "exec",
		env: []corev1.EnvVar{{
			Name:  "READY_DELAY",
			Value: "10s",
		}},
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

// This test validates the behaviour of readiness probes *after* initial startup.
// The current behaviour is not ideal: when a pod goes unready after startup
// and there are no other pods in the revision we hang, potentially forever,
// which may not be what a user wants.
// See https://github.com/knative/serving/issues/10765.
func TestProbeRuntimeAfterStartup(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Readiness,
	}

	test.EnsureTearDown(t, clients, &names)
	resources, err := v1test.CreateServiceReady(t, clients, &names, v1opts.WithReadinessProbe(
		&corev1.Probe{
			// This behaviour is only the case where periodSeconds=0, because probes
			// are ignored after startup when periodSeconds>0
			// See https://github.com/knative/serving/issues/10764.
			PeriodSeconds: 0,
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
				},
			},
		}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// This WaitForEndpointState is mostly here to account for non-conformant
	// network implementations which may not actually be "ready" immediately
	// after reporting Ready (e.g. if they use dns).
	// Ref: https://github.com/knative/serving/issues/11404
	t.Log("Wait for initial readiness")
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

	url.Path = "/"
	// When periodSeconds = 0 we expect that once readiness propagates
	// there will be no ready pods, and therefore the request will time
	// out. (see https://github.com/knative/serving/issues/10765).
	if err := wait.PollImmediate(1*time.Second, readinessPropagationTime, func() (bool, error) {
		startFailing, err := http.NewRequest(http.MethodGet, url.String(), nil)
		if err != nil {
			return false, err
		}

		// 5 Seconds is enough to be confident the request is timing out.
		client.Client.Timeout = 5 * time.Second

		resp, err := client.Do(startFailing, func(err error) (bool, error) {
			if isTimeout(err) {
				// We're actually expecting a timeout here, so don't retry on timeouts.
				return false, nil
			}

			return spoof.DefaultErrorRetryChecker(err)
		})
		if isTimeout(err) {
			// We expect to eventually time out, so this is the success case.
			return true, nil
		} else if err != nil {
			// Other errors are not expected.
			return false, err
		} else if resp.StatusCode == http.StatusOK {
			// We'll continue to get 200s for a while until readiness propagates.
			return false, nil
		}

		return false, errors.New("Received non-200 status code (expected to eventually time out)")
	}); err != nil {
		t.Fatal("Expected to eventually see request timeout due to all pods becoming unready, but got:", err)
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
