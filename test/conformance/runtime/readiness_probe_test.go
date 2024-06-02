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
	"net/url"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	// readinessPropagationTime is how long to poll to allow for readiness probe
	// changes to propagate to ingresses/activator.
	//
	// When Readiness.PeriodSeconds=0 the underlying Pods use the K8s
	// defaults for readiness. Those are:
	// - Readiness.PeriodSeconds=10
	// - Readiness.FailureThreshold=3
	//
	// Thus it takes at a mininum 30 seconds for the Pod to become
	// unready. To account for this we bump max propagation time
	readinessPropagationTime = time.Minute
	readinessPath            = "/healthz/readiness"
)

func TestProbeRuntime(t *testing.T) {
	t.Parallel()
	if test.ServingFlags.DisableOptionalAPI {
		t.Skip("Container.readinessProbe is not required by Knative Serving API Specification")
	}
	clients := test.Setup(t)

	var testCases = []struct {
		// name of the test case, which will be inserted in names of routes, configurations, etc.
		// Use a short name here to avoid hitting the 63-character limit in names
		// (e.g., "service-to-service-call-svc-cluster-local-uagkdshh-frkml-service" is too long.)
		name    string
		handler corev1.ProbeHandler
		env     []corev1.EnvVar
	}{{
		name: "httpGet",
		env: []corev1.EnvVar{{
			Name:  "READY_DELAY",
			Value: "10s",
		}},
		handler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: readinessPath,
			},
		},
	}, {
		name: "tcpSocket",
		env: []corev1.EnvVar{{
			Name:  "LISTEN_DELAY",
			Value: "10s",
		}},
		handler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{},
		},
	}, {
		name: "exec",
		env: []corev1.EnvVar{{
			Name:  "READY_DELAY",
			Value: "10s",
		}},
		handler: corev1.ProbeHandler{
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
							ProbeHandler:  tc.handler,
							PeriodSeconds: period,
						}))
				if err != nil {
					t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
				}

				// Once the service reports ready we should immediately be able to curl it.
				url := resources.Route.Status.URL.URL()
				url.Path = readinessPath
				if _, err = pkgtest.CheckEndpointState(
					context.Background(),
					clients.KubeClient,
					t.Logf,
					url,
					spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText)),
					"readinessIsReady",
					test.ServingFlags.ResolvableDomain,
					test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
					spoof.WithHeader(test.ServingFlags.RequestHeader()),
				); err != nil {
					t.Fatalf("The endpoint for Route %s at %s didn't return success: %v", names.Route, url, err)
				}
			})
		}
	}
}

// This test validates the behaviour of readiness probes *after* initial
// startup. When a pod goes unready after startup and there are no other pods
// in the revision, then there are two possible behaviors:
//  1. When the Activator is present we hang, potentially forever, which may or
//     may not be what a user wants.
//  2. When the Activator is not present, we see a 5xx.
//
// The goal of this test is largely to describe the current behaviour, so that
// we can confidently change it.
// See https://github.com/knative/serving/issues/10765.
func TestProbeRuntimeAfterStartup(t *testing.T) {
	t.Parallel()
	if test.ServingFlags.DisableOptionalAPI {
		t.Skip("Container.readinessProbe behaviour after startup is not defined by Knative Serving API Specification")
	}

	for _, period := range []int32{0, 1} {
		period := period
		t.Run(fmt.Sprintf("periodSeconds=%d", period), func(t *testing.T) {
			t.Parallel()
			clients := test.Setup(t)
			names := test.ResourceNames{Service: test.ObjectNameForTest(t), Image: test.Readiness}
			test.EnsureTearDown(t, clients, &names)

			url, client := waitReadyThenStartFailing(t, clients, names, period)
			if err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, readinessPropagationTime, true, func(context.Context) (bool, error) {
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
				} else if resp.StatusCode == http.StatusServiceUnavailable {
					// When the activator isn't on the request path, we expect
					// the service to serve 503s when all the endpoints become
					// unavailable.
					return true, nil
				}

				return false, fmt.Errorf("Received non-200, non-503 status code: %d, wanted request to time out.\nBody: %s", resp.StatusCode, string(resp.Body))
			}); err != nil {
				t.Fatal("Expected to eventually see request timeout due to all pods becoming unready, but got:", err)
			}
		})
	}
}

// waitReadyThenStartFailing creates a service, waits for it to pass readiness,
// and then causes its readiness test to start failing. It returns the URL of
// an endpoint on the created service, and an appropriate spoofing client to
// use to access it.
func waitReadyThenStartFailing(t *testing.T, clients *test.Clients, names test.ResourceNames, probePeriod int32) (*url.URL, *spoof.SpoofingClient) {
	resources, err := v1test.CreateServiceReady(t, clients, &names, v1opts.WithReadinessProbe(
		&corev1.Probe{
			PeriodSeconds: probePeriod,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: readinessPath,
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
	if _, err = pkgtest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText)),
		"readinessIsReady",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
		spoof.WithHeader(test.ServingFlags.RequestHeader()),
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
	return url, client
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
