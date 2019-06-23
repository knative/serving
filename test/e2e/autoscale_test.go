// +build e2e

/*
Copyright 2018 The Knative Authors

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
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logstream"
	"github.com/knative/serving/pkg/apis/autoscaling"
	resourcenames "github.com/knative/serving/pkg/reconciler/revision/resources/names"
	rtesting "github.com/knative/serving/pkg/testing/v1alpha1"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	autoscaleExpectedOutput = "399989"
	// Concurrency must be high enough to avoid the problems with sampling
	// but not high enough to generate scheduling problems.
	containerConcurrency = 6
)

func generateTraffic(ctx *testContext, concurrency int, duration time.Duration, stopChan chan struct{}) error {
	var (
		totalRequests      int32
		successfulRequests int32
		group              errgroup.Group
	)

	ctx.t.Logf("Maintaining %d concurrent requests for %v.", concurrency, duration)
	client, err := pkgTest.NewSpoofingClient(ctx.clients.KubeClient, ctx.t.Logf, ctx.domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return fmt.Errorf("error creating spoofing client: %v", err)
	}
	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s?sleep=100", ctx.domain), nil)
			if err != nil {
				return fmt.Errorf("error creating HTTP request: %v", err)
			}
			done := time.After(duration)
			for {
				select {
				case <-stopChan:
					ctx.t.Log("Stopping generateTraffic")
					return nil
				case <-done:
					ctx.t.Log("Time is up; done")
					return nil
				default:
					atomic.AddInt32(&totalRequests, 1)
					res, err := client.Do(req)
					if err != nil {
						ctx.t.Logf("Error making request %v", err)
						continue
					}

					if res.StatusCode != http.StatusOK {
						ctx.t.Logf("Status = %d, want: %d", res.StatusCode, http.StatusOK)
						ctx.t.Logf("Response: %s", res)
						continue
					}
					atomic.AddInt32(&successfulRequests, 1)
				}
			}
		})
	}

	ctx.t.Log("Waiting for all requests to complete.")
	if err := group.Wait(); err != nil {
		return fmt.Errorf("error making requests for scale up: %v", err)
	}

	if successfulRequests != totalRequests {
		return fmt.Errorf("error making requests for scale up. Got %d successful requests, wanted: %d",
			successfulRequests, totalRequests)
	}
	return nil
}

type testContext struct {
	t              *testing.T
	clients        *test.Clients
	names          test.ResourceNames
	deploymentName string
	domain         string
}

func setup(t *testing.T, class string, metric string) *testContext {
	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "autoscale",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{
		ContainerConcurrency: containerConcurrency,
	}, rtesting.WithConfigAnnotations(map[string]string{
		autoscaling.ClassAnnotationKey:  class,
		autoscaling.MetricAnnotationKey: metric,
	}), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("300Mi"),
		},
	}),
	)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	domain := resources.Route.Status.URL.Host
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		// Istio doesn't expose a status for us here: https://github.com/istio/istio/issues/6082
		// TODO(tcnghia): Remove this when https://github.com/istio/istio/issues/882 is fixed.
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(autoscaleExpectedOutput))),
		"CheckingEndpointAfterUpdating",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%v\": %v",
			names.Route, domain, autoscaleExpectedOutput, err)
	}

	return &testContext{
		t:              t,
		clients:        clients,
		names:          names,
		deploymentName: resourcenames.Deployment(resources.Revision),
		domain:         domain,
	}
}

func assertScaleDown(ctx *testContext) {
	if err := WaitForScaleToZero(ctx.t, ctx.deploymentName, ctx.clients); err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", ctx.deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	ctx.t.Log("Wait for all pods to terminate.")

	if err := pkgTest.WaitForPodListState(
		ctx.clients.KubeClient,
		func(p *v1.PodList) (bool, error) {
			for _, pod := range p.Items {
				if strings.Contains(pod.Name, ctx.deploymentName) &&
					!strings.Contains(pod.Status.Reason, "Evicted") {
					return false, nil
				}
			}
			return true, nil
		},
		"WaitForAvailablePods", test.ServingNamespace); err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods of %q: %v", ctx.deploymentName, err)
	}

	ctx.t.Log("The Revision should remain ready after scaling to zero.")
	if err := v1a1test.CheckRevisionState(ctx.clients.ServingAlphaClient, ctx.names.Revision, v1a1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to zero: %v", ctx.names.Revision, err)
	}

	ctx.t.Log("Scaled down.")
}

func numberOfPods(ctx *testContext) (int32, error) {
	deployment, err := ctx.clients.KubeClient.Kube.Apps().Deployments(test.ServingNamespace).Get(ctx.deploymentName, metav1.GetOptions{})
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to get deployment %q", deployment)
	}
	return deployment.Status.ReadyReplicas, nil
}

func assertAutoscaleUpToNumPods(ctx *testContext, curPods, targetPods int32, duration time.Duration, quick bool) {
	// There are two test modes: quick, and not quick.
	// 1) Quick mode: succeeds when the number of pods meets targetPods.
	// 2) Not Quick (sustaining) mode: succeeds when the number of pods gets scaled to targetPods and
	//    sustains there for the `duration`.

	// Relax the bounds to reduce the flakiness caused by sampling in the autoscaling algorithm.
	minPods := curPods - 1
	maxPods := targetPods + 1

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		return generateTraffic(ctx, int(targetPods*containerConcurrency), duration, stopChan)
	})
	grp.Go(func() error {
		// Short-circuit traffic generation once we exit from the check logic.
		defer close(stopChan)

		done := time.After(duration)
		timer := time.Tick(2 * time.Second)
		for {
			select {
			case <-timer:
				// Each 2 second, check that the number of pods is at least `minPods`. `minPods` is increasing
				// to verify that the number of pods doesn't go down while we are scaling up.
				got, err := numberOfPods(ctx)
				if err != nil {
					return err
				}
				mes := fmt.Sprintf("deployment '%s' #replicas: %d, want at least: %d", ctx.deploymentName, got, minPods)
				ctx.t.Log(mes)
				if got < minPods {
					return errors.New(mes)
				}
				if quick {
					// A quick test succeeds when the number of pods scales up to `targetPods`
					// (and, for sanity check, no more than `maxPods`).
					if got >= targetPods && got <= maxPods {
						ctx.t.Logf("got %d replicas, reached target of %d, exiting early", got, targetPods)
						return nil
					}
				}
				if minPods < targetPods-1 {
					// Increase `minPods`, but leave room to reduce flakiness.
					minPods = int32(math.Min(float64(got), float64(targetPods))) - 1
				}
			case <-done:
				// The test duration is over. Do a last check to verify that the number of pods is at `targetPods`
				// (with a little room for de-flakiness).
				got, err := numberOfPods(ctx)
				if err != nil {
					return err
				}
				mes := fmt.Sprintf("got %d replicas, expected between [%d, %d] replicas for deployment %s", got, targetPods-1, maxPods, ctx.deploymentName)
				ctx.t.Log(mes)
				if got < targetPods-1 || got > maxPods {
					return errors.New(mes)
				}
				return nil
			}
		}
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Error(err)
	}
}

func TestAutoscaleUpDownUp(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency)
	defer test.TearDown(ctx.clients, ctx.names)

	assertAutoscaleUpToNumPods(ctx, 1, 2, 60*time.Second, true)
	assertScaleDown(ctx)
	assertAutoscaleUpToNumPods(ctx, 0, 2, 60*time.Second, true)
}

func TestAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()

	classes := map[string]string{
		"hpa": autoscaling.HPA,
		"kpa": autoscaling.KPA,
	}

	for name, class := range classes {
		name, class := name, class
		t.Run(name, func(tt *testing.T) {
			tt.Parallel()
			cancel := logstream.Start(t)
			defer cancel()

			ctx := setup(tt, class, autoscaling.Concurrency)
			defer test.TearDown(ctx.clients, ctx.names)

			ctx.t.Log("The autoscaler spins up additional replicas when traffic increases.")
			// note: without the warm-up / gradual increase of load the test is retrieving a 503 (overload) from the envoy

			// Increase workload for 2 replicas for 60s
			// Assert the number of expected replicas is between n-1 and n+1, where n is the # of desired replicas for 60s.
			// Assert the number of expected replicas is n and n+1 at the end of 60s, where n is the # of desired replicas.
			assertAutoscaleUpToNumPods(ctx, 1, 2, 60*time.Second, true)
			// Increase workload scale to 3 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
			assertAutoscaleUpToNumPods(ctx, 2, 3, 60*time.Second, true)
			// Increase workload scale to 4 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
			assertAutoscaleUpToNumPods(ctx, 3, 4, 60*time.Second, true)
		})
	}
}

func TestAutoscaleSustaining(t *testing.T) {
	// When traffic increases, a knative app should scale up and sustain the scale
	// as long as the traffic sustains, despite whether it is switching modes between
	// normal and panic.
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency)
	defer test.TearDown(ctx.clients, ctx.names)

	assertAutoscaleUpToNumPods(ctx, 1, 10, 3*time.Minute, false)
}
