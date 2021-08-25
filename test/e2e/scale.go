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
	"net/url"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/pool"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
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

// ScaleToWithin creates `scale` services in parallel subtests and reports the
// time taken to `latencies`.
func ScaleToWithin(t *testing.T, scale int, duration time.Duration, latencies Latencies) {
	// These are the local (per-probe) and global (all probes) targets for the scale test.
	// 95 = 19/20, so allow one failure within the minimum number of probes, but expect
	// us to have 3 9s overall.
	const (
		localSLO  = 1.0
		minProbes = 20
	)

	// wg tracks the number of ksvcs that we are still waiting to see become ready.
	wg := sync.WaitGroup{}

	// Note the number of times we expect Done() to be called.
	wg.Add(scale)
	eg := pool.NewWithCapacity(50 /* maximum in-flight creates */, scale /* capacity */)

	t.Cleanup(func() {
		// Wait for the ksvcs to all become ready (or fail)
		wg.Wait()

		// Wait for all of the service creations to complete (possibly in failure),
		// and signal the done channel.
		if err := eg.Wait(); err != nil {
			t.Error("Wait() =", err)
		}

		// TODO(mattmoor): Check globalSLO if localSLO isn't 1.0
	})

	for i := 0; i < scale; i++ {
		// https://golang.org/doc/faq#closures_and_goroutines
		i := i
		t.Run(fmt.Sprintf("%03d-of-%03d", i, scale), func(t *testing.T) {
			t.Parallel()

			clients := Setup(t)
			pm := test.NewProberManager(t.Logf, clients, minProbes, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))

			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   "helloworld",
			}

			t.Cleanup(func() {
				// Wait for all ksvcs to have been created before exiting the test.
				t.Log("Waiting for all to become ready")
				wg.Wait()
				pm.Stop()

				pm.Foreach(func(u *url.URL, p test.Prober) {
					if err := test.CheckSLO(localSLO, u.String(), p); err != nil {
						t.Error("CheckSLO() =", err)
					}
				})

				test.TearDown(clients, &names)
			})

			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), duration)

				// This go routine runs until the ksvc is ready, at which point we should note that
				// our part is done.
				defer func() {
					wg.Done()
					t.Logf("Reporting done!")
					cancel()
				}()

				// Start the clock for various waypoints towards Service readiness.
				start := time.Now()
				// Record the overall completion time regardless of success/failure.
				defer latencies.Add("time-to-done", start)

				_, err := v1test.CreateService(t, clients, names,
					func(svc *v1.Service) {
						svc.Spec.Template.Spec.EnableServiceLinks = ptr.Bool(false)
					},
					rtesting.WithResourceRequirements(corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("30m"),
							corev1.ResourceMemory: resource.MustParse("20Mi"),
						},
					}),
					rtesting.WithConfigAnnotations(map[string]string{
						autoscaling.MaxScaleAnnotationKey: "1",
					}),
					rtesting.WithReadinessProbe(&corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/",
							},
						},
					}),
					rtesting.WithRevisionTimeoutSeconds(10))
				if err != nil {
					t.Error("CreateService() =", err)
					return fmt.Errorf("CreateService() failed: %w", err)
				}

				// Record the time it took to create the service.
				latencies.Add("time-to-create", start)

				t.Logf("Wait for %s to become ready.", names.Service)
				var url *url.URL
				err = v1test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1.Service) (bool, error) {
					if ctx.Err() != nil {
						return false, ctx.Err()
					}
					if s.Status.URL == nil {
						return false, nil
					}
					url = s.Status.URL.URL()
					return v1test.IsServiceReady(s)
				}, "ServiceUpdatedWithURL")

				if err != nil {
					t.Error("WaitForServiceState(w/ Domain) =", err)
					return fmt.Errorf("WaitForServiceState(w/ Domain) failed: %w", err)
				}

				// Record the time it took to become ready.
				latencies.Add("time-to-ready", start)

				_, err = pkgTest.WaitForEndpointState(
					context.Background(),
					clients.KubeClient,
					t.Logf,
					url,
					v1test.RetryingRouteInconsistency(spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText), abortOnTimeout(ctx))),
					"WaitForEndpointToServeText",
					test.ServingFlags.ResolvableDomain,
					test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
				if err != nil {
					t.Error("WaitForEndpointState(expected text) =", err)
					return fmt.Errorf("WaitForEndpointState(expected text) failed: %w", err)
				}

				// Record the time it took to get back a 200 with the expected text.
				latencies.Add("time-to-200", start)

				// Start probing the domain until the test is complete.
				pm.Spawn(url)

				t.Logf("%s is ready.", names.Service)
				return nil
			})
		})
	}
}
