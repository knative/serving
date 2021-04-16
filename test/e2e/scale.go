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

	"golang.org/x/sync/semaphore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/helpers"
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
	clients := Setup(t)

	// These are the local (per-probe) and global (all probes) targets for the scale test.
	// 95 = 19/20, so allow one failure within the minimum number of probes, but expect
	// us to have 3 9s overall.
	const (
		localSLO  = 1.0
		minProbes = 20
	)

	var (
		// createWg tracks the number of ksvcs that we are still waiting to see become ready.
		createWg sync.WaitGroup
		// checkWg tracks the number of checks we're still waiting to happen.
		checkWg sync.WaitGroup
	)

	// Note the number of times we expect Done() to be called.
	createWg.Add(scale)
	checkWg.Add(scale)
	sem := semaphore.NewWeighted(50) // limit the in-flight creates
	errCh := make(chan error, scale)

	prefix := helpers.AppendRandomString(helpers.ObjectPrefixForTest(t))
	for i := 0; i < scale; i++ {
		// https://golang.org/doc/faq#closures_and_goroutines
		i := i
		go func() {
			defer checkWg.Done()

			pm := test.NewProberManager(t.Logf, clients, minProbes, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))

			names := test.ResourceNames{
				Service: kmeta.ChildName(prefix, fmt.Sprintf("-%03d-of-%03d", i+1, scale)),
				Image:   "helloworld",
			}

			if err := func() error {
				defer createWg.Done()

				ctx, cancel := context.WithTimeout(context.Background(), duration)
				defer cancel()

				// Acquire a slot in the semaphore to throttle the in-flight creates.
				if err := sem.Acquire(ctx, 1); err != nil {
					return fmt.Errorf("%s: Acquire(): %w", names.Service, err)
				}
				defer sem.Release(1)

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
					return fmt.Errorf("%s: CreateService(): %w", names.Service, err)
				}
				test.EnsureTearDown(t, clients, &names)

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
					return fmt.Errorf("%s: WaitForServiceState(w/ Domain): %w", names.Service, err)
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
					test.ServingFlags.ResolvableDomain)
				if err != nil {
					return fmt.Errorf("%s: WaitForEndpointState(expected text): %w", names.Service, err)
				}

				// Record the time it took to get back a 200 with the expected text.
				latencies.Add("time-to-200", start)

				// Start probing the domain until the test is complete.
				pm.Spawn(url)

				t.Logf("%s is ready.", names.Service)
				return nil
			}(); err != nil {
				errCh <- err
				return
			}

			// Probe for the entire time, until all services are ready (or failed).
			t.Log("Waiting for all services to become ready")
			createWg.Wait()

			pm.Stop()
			pm.Foreach(func(u *url.URL, p test.Prober) {
				if err := test.CheckSLO(localSLO, u.String(), p); err != nil {
					errCh <- fmt.Errorf("%s: CheckSLO(): %w", names.Service, err)
				}
			})
		}()
	}

	// Wait for all checks to finish
	checkWg.Wait()
	close(errCh)

	failed := 0
	for err := range errCh {
		t.Error(err.Error())
		failed++
	}
	if failed > 0 {
		t.Errorf("%d services failed in total", failed)
	}

	// TODO(mattmoor): Check globalSLO if localSLO isn't 1.0
}
