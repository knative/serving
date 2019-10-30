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

package e2e

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/pool"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	v1alpha1testing "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

// Latencies is an interface for providing mechanisms for recording timings
// for the parts of the scale test.
type Latencies interface {
	// Add takes the name of this measurement and the time at which it began.
	// This should be called at the moment of completion, so that duration may
	// be computed with `time.Since(start)`.  We use this signature to that this
	// function is suitable for use in a `defer`.
	Add(name string, start time.Time)
}

func abortOnTimeout(ctx context.Context) spoof.ResponseChecker {
	return func(resp *spoof.Response) (bool, error) {
		return true, ctx.Err()
	}
}

func ScaleToWithin(t *testing.T, scale int, duration time.Duration, latencies Latencies) {
	clients := Setup(t)

	cleanupCh := make(chan test.ResourceNames, scale)
	defer close(cleanupCh)

	// These are the local (per-probe) and global (all probes) targets for the scale test.
	// 90 = 18/20, so allow two failures with the minimum number of probes, but expect
	// us to have 2.5 9s overall.
	//
	// TODO(#2850): After moving to Istio 1.1 we need to revisit these SLOs.
	const (
		localSLO  = 0.90
		globalSLO = 0.995
		minProbes = 20
	)
	pm := test.NewProberManager(t.Logf, clients, minProbes)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	width := int(math.Ceil(math.Log10(float64(scale))))

	t.Log("Creating new Services")
	wg := pool.NewWithCapacity(50 /* maximum in-flight creates */, scale /* capacity */)
	for i := 0; i < scale; i++ {
		// https://golang.org/doc/faq#closures_and_goroutines
		i := i
		wg.Go(func() error {
			names := test.ResourceNames{
				Service: test.SubServiceNameForTest(t, fmt.Sprintf("%0[1]*[2]d", width, i)),
				Image:   "helloworld",
			}

			// Start the clock for various waypoints towards Service readiness.
			start := time.Now()
			// Record the overall completion time regardless of success/failure.
			defer latencies.Add("time-to-done", start)

			svc, err := v1a1test.CreateLatestService(t, clients, names,
				v1alpha1testing.WithResourceRequirements(corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("50Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("20Mi"),
					},
				}),
				v1alpha1testing.WithConfigAnnotations(map[string]string{
					"autoscaling.knative.dev/maxScale": "1",
				}),
				v1alpha1testing.WithReadinessProbe(&corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				}),
				v1alpha1testing.WithRevisionTimeoutSeconds(10))

			if err != nil {
				t.Errorf("CreateLatestService() = %v", err)
				return fmt.Errorf("CreateLatestService() failed: %w", err)
			}
			// Record the time it took to create the service.
			latencies.Add("time-to-create", start)
			names.Route = serviceresourcenames.Route(svc)
			names.Config = serviceresourcenames.Configuration(svc)

			// Send it to our cleanup logic (below)
			cleanupCh <- names

			t.Logf("Wait for %s to become ready.", names.Service)
			var url *url.URL
			err = v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				if s.Status.URL == nil {
					return false, nil
				}
				url = s.Status.URL.URL()
				return v1a1test.IsServiceReady(s)
			}, "ServiceUpdatedWithURL")
			if err != nil {
				t.Errorf("WaitForServiceState(w/ Domain) = %v", err)
				return fmt.Errorf("WaitForServiceState(w/ Domain) failed: %w", err)
			}

			// Record the time it took to become ready.
			latencies.Add("time-to-ready", start)

			_, err = pkgTest.WaitForEndpointState(
				clients.KubeClient,
				t.Logf,
				url,
				v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.HelloWorldText), abortOnTimeout(ctx))),
				"WaitForEndpointToServeText",
				test.ServingFlags.ResolvableDomain)
			if err != nil {
				t.Errorf("WaitForEndpointState(expected text) = %v", err)
				return fmt.Errorf("WaitForEndpointState(expected text) failed: %w", err)
			}

			// Record the time it took to get back a 200 with the expected text.
			latencies.Add("time-to-200", start)

			// Start probing the domain until the test is complete.
			pm.Spawn(url)

			t.Logf("%s is ready.", names.Service)
			return nil
		})
	}

	// Wait for all of the service creations to complete (possibly in failure),
	// and signal the done channel.
	doneCh := make(chan error)
	go func() {
		defer close(doneCh)
		if err := wg.Wait(); err != nil {
			doneCh <- err
		}
	}()

	for {
		// As services get created, add logic to clean them up.
		// When all of the creations have finished, then stop all of the active probers
		// and check our SLIs against our SLOs.
		// All of this has to finish within the configured timeout.
		select {
		case names := <-cleanupCh:
			t.Logf("Added %v to cleanup routine.", names)
			test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
			defer test.TearDown(clients, names)

		case err := <-doneCh:
			if err != nil {
				// If we don't do this first, then we'll see tons of 503s from the ongoing probes
				// as we tear down the things they are probing.
				defer pm.Stop()
				t.Fatalf("Unexpected error: %v", err)
			}

			// This ProberManager implementation waits for minProbes before actually stopping.
			if err := pm.Stop(); err != nil {
				t.Fatalf("Stop() = %v", err)
			}
			// Check each of the local SLOs
			pm.Foreach(func(u *url.URL, p test.Prober) {
				if err := test.CheckSLO(localSLO, u.String(), p); err != nil {
					t.Errorf("CheckSLO() = %v", err)
				}
			})
			// Check the global SLO
			if err := test.CheckSLO(globalSLO, "aggregate", pm); err != nil {
				t.Errorf("CheckSLO() = %v", err)
			}
			return
		}
	}
}
