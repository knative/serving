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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	vegeta "github.com/tsenart/vegeta/lib"
	"golang.org/x/sync/errgroup"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	ingress "knative.dev/pkg/test/ingress"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	// Concurrency must be high enough to avoid the problems with sampling
	// but not high enough to generate scheduling problems.
	containerConcurrency = 6.0
	targetUtilization    = 0.7
	successRateSLO       = 0.999
)

type testContext struct {
	t                 *testing.T
	clients           *test.Clients
	names             test.ResourceNames
	resources         *v1a1test.ResourceObjects
	targetUtilization float64
	targetValue       float64
	metric            string
}

func getVegetaTarget(kubeClientset *kubernetes.Clientset, domain, endpointOverride string, resolvable bool) (vegeta.Target, error) {
	if resolvable {
		return vegeta.Target{
			Method: http.MethodGet,
			URL:    fmt.Sprintf("http://%s?sleep=100", domain),
		}, nil
	}

	endpoint := endpointOverride
	if endpointOverride == "" {
		var err error
		// If the domain that the Route controller is configured to assign to Route.Status.Domain
		// (the domainSuffix) is not resolvable, we need to retrieve the endpoint and spoof
		// the Host in our requests.
		if endpoint, err = ingress.GetIngressEndpoint(kubeClientset); err != nil {
			return vegeta.Target{}, err
		}
	}

	h := http.Header{}
	h.Set("Host", domain)
	return vegeta.Target{
		Method: http.MethodGet,
		URL:    fmt.Sprintf("http://%s?sleep=100", endpoint),
		Header: h,
	}, nil
}

func generateTraffic(
	ctx *testContext,
	attacker *vegeta.Attacker,
	pacer vegeta.Pacer,
	duration time.Duration,
	stopChan chan struct{}) error {

	target, err := getVegetaTarget(
		ctx.clients.KubeClient.Kube, ctx.resources.Route.Status.URL.URL().Hostname(), pkgTest.Flags.IngressEndpoint, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return fmt.Errorf("error creating vegeta target: %w", err)
	}

	results := attacker.Attack(vegeta.NewStaticTargeter(target), pacer, duration, "load-test")
	defer attacker.Stop()

	var (
		totalRequests      int32
		successfulRequests int32
	)
	for {
		select {
		case <-stopChan:
			ctx.t.Log("Stopping generateTraffic")
			successRate := float64(1)
			if totalRequests > 0 {
				successRate = float64(successfulRequests) / float64(totalRequests)
			}
			if successRate < successRateSLO {
				return fmt.Errorf("request success rate under SLO: total = %d, errors = %d, rate = %f, SLO = %f",
					totalRequests, totalRequests-successfulRequests, successRate, successRateSLO)
			}
			return nil
		case res, ok := <-results:
			if !ok {
				ctx.t.Log("Time is up; done")
				return nil
			}

			totalRequests++
			if res.Code != http.StatusOK {
				ctx.t.Logf("Status = %d, want: 200", res.Code)
				ctx.t.Logf("Response: %v", res)
				continue
			}
			successfulRequests++
		}
	}
}

func generateTrafficAtFixedConcurrency(ctx *testContext, concurrency int, duration time.Duration, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{} // Sends requests as quickly as possible, capped by MaxWorkers below.
	attacker := vegeta.NewAttacker(vegeta.Timeout(duration), vegeta.Workers(uint64(concurrency)), vegeta.MaxWorkers(uint64(concurrency)))

	ctx.t.Logf("Maintaining %d concurrent requests for %v.", concurrency, duration)
	return generateTraffic(ctx, attacker, pacer, duration, stopChan)
}

func generateTrafficAtFixedRPS(ctx *testContext, rps int, duration time.Duration, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{Freq: rps, Per: time.Second}
	attacker := vegeta.NewAttacker(vegeta.Timeout(duration))

	ctx.t.Logf("Maintaining %v RPS requests for %v.", rps, duration)
	return generateTraffic(ctx, attacker, pacer, duration, stopChan)
}

// setup creates a new service, with given service options.
// It returns a testContext that has resources, K8s clients and other needed
// data points.
// It sets up CleanupOnInterrupt as well that will destroy the resources
// when the test terminates.
func setup(t *testing.T, class, metric string, target float64, targetUtilization float64, fopts ...rtesting.ServiceOption) *testContext {
	t.Helper()
	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "autoscale",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	resources, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		append(fopts, rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.ClassAnnotationKey:             class,
			autoscaling.MetricAnnotationKey:            metric,
			autoscaling.TargetAnnotationKey:            strconv.FormatFloat(target, 'f', -1, 64),
			autoscaling.TargetUtilizationPercentageKey: strconv.FormatFloat(targetUtilization*100, 'f', -1, 64),
		}), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("300Mi"),
			},
		}))...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK)),
		"CheckingEndpointAfterUpdating",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing %s: %v", url, err)
	}

	return &testContext{
		t:                 t,
		clients:           clients,
		names:             names,
		resources:         resources,
		targetUtilization: targetUtilization,
		targetValue:       target,
		metric:            metric,
	}
}

func assertScaleDown(ctx *testContext) {
	deploymentName := resourcenames.Deployment(ctx.resources.Revision)
	if err := WaitForScaleToZero(ctx.t, deploymentName, ctx.clients); err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	ctx.t.Log("Wait for all pods to terminate.")

	if err := pkgTest.WaitForPodListState(
		ctx.clients.KubeClient,
		func(p *corev1.PodList) (bool, error) {
			for _, pod := range p.Items {
				if strings.Contains(pod.Name, deploymentName) &&
					!strings.Contains(pod.Status.Reason, "Evicted") {
					return false, nil
				}
			}
			return true, nil
		},
		"WaitForAvailablePods", test.ServingNamespace); err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods of %q: %v", deploymentName, err)
	}

	ctx.t.Log("The Revision should remain ready after scaling to zero.")
	if err := v1a1test.CheckRevisionState(ctx.clients.ServingAlphaClient, ctx.names.Revision, v1a1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to zero: %v", ctx.names.Revision, err)
	}

	ctx.t.Log("Scaled down.")
}

func numberOfPods(ctx *testContext) (float64, error) {
	// SKS name matches that of revision.
	n := ctx.resources.Revision.Name
	sks, err := ctx.clients.NetworkingClient.ServerlessServices.Get(n, metav1.GetOptions{})
	if err != nil {
		ctx.t.Logf("Error getting SKS %q: %v", n, err)
		return 0, fmt.Errorf("error retrieving sks %q: %w", n, err)
	}
	if sks.Status.PrivateServiceName == "" {
		ctx.t.Logf("SKS %s has not yet reconciled", n)
		// Not an error, but no pods either.
		return 0, nil
	}
	eps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(
		sks.Status.PrivateServiceName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get endpoints %s: %w", sks.Status.PrivateServiceName, err)
	}
	return float64(resources.ReadyAddressCount(eps)), nil
}

func assertAutoscaleUpToNumPods(ctx *testContext, curPods, targetPods float64, duration time.Duration, quick bool) {
	ctx.t.Helper()
	// There are two test modes: quick, and not quick.
	// 1) Quick mode: succeeds when the number of pods meets targetPods.
	// 2) Not Quick (sustaining) mode: succeeds when the number of pods gets scaled to targetPods and
	//    sustains there for the `duration`.

	// Relax the bounds to reduce the flakiness caused by sampling in the autoscaling algorithm.
	// Also adjust the values by the target utilization values.

	minPods := math.Floor(curPods/ctx.targetUtilization) - 1
	maxPods := math.Ceil(targetPods/ctx.targetUtilization) + 1

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		switch ctx.metric {
		case autoscaling.RPS:
			return generateTrafficAtFixedRPS(ctx, int(targetPods*ctx.targetValue), duration, stopChan)
		default:
			return generateTrafficAtFixedConcurrency(ctx, int(targetPods*ctx.targetValue), duration, stopChan)
		}
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
				mes := fmt.Sprintf("revision '%s' #replicas: %v, want at least: %v", ctx.resources.Revision.Name, got, minPods)
				ctx.t.Log(mes)
				if got < minPods {
					return errors.New(mes)
				}
				if quick {
					// A quick test succeeds when the number of pods scales up to `targetPods`
					// (and, for sanity check, no more than `maxPods`).
					if got >= targetPods && got <= maxPods {
						ctx.t.Logf("Got %v replicas, reached target of %v, exiting early", got, targetPods)
						return nil
					}
				}
				if minPods < targetPods-1 {
					// Increase `minPods`, but leave room to reduce flakiness.
					minPods = math.Min(got, targetPods) - 1
				}
			case <-done:
				// The test duration is over. Do a last check to verify that the number of pods is at `targetPods`
				// (with a little room for de-flakiness).
				got, err := numberOfPods(ctx)
				if err != nil {
					return err
				}
				mes := fmt.Sprintf("got %v replicas, expected between [%v, %v] replicas for revision %s",
					got, targetPods-1, maxPods, ctx.resources.Revision.Name)
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

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization)
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
			cancel := logstream.Start(tt)
			defer cancel()

			ctx := setup(tt, class, autoscaling.Concurrency, containerConcurrency, targetUtilization)
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

func TestRPSBasedAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()

	classes := map[string]string{
		"hpa": autoscaling.HPA,
		"kpa": autoscaling.KPA,
	}

	for name, class := range classes {
		name, class := name, class
		t.Run(name, func(tt *testing.T) {
			tt.Parallel()
			cancel := logstream.Start(tt)
			defer cancel()

			ctx := setup(tt, class, autoscaling.RPS, 10, targetUtilization)
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

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization)
	defer test.TearDown(ctx.clients, ctx.names)

	assertAutoscaleUpToNumPods(ctx, 1, 10, 2*time.Minute, false)
}

func TestTargetBurstCapacity(t *testing.T) {
	// This test sets up a service with CC=10 TU=70% and TBC=7.
	// Then sends requests at concurrency causing activator in the path.
	// Then at the higher concurrency 10,
	// getting spare capacity of 20-10=10, which should remove the
	// Activator from the request path.
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, 10 /* target concurrency*/, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey:                "7",
			autoscaling.PanicThresholdPercentageAnnotationKey: "200", // makes panicking rare
		}))
	defer test.TearDown(ctx.clients, ctx.names)

	cfg, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatalf("Error retrieving autoscaler configmap: %v", err)
	}
	var (
		grp    errgroup.Group
		stopCh = make(chan struct{})
	)
	defer grp.Wait()
	defer close(stopCh)

	// We'll terminate the test via stopCh.
	const duration = time.Hour

	grp.Go(func() error {
		return generateTrafficAtFixedConcurrency(ctx, 7, duration, stopCh)
	})

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(ctx.resources, ctx.clients); err != nil {
		t.Fatalf("Never got Activator endpoints in the service: %v", err)
	}

	// Start second load generator.
	grp.Go(func() error {
		return generateTrafficAtFixedConcurrency(ctx, 5, duration, stopCh)
	})

	// Wait for two stable pods.
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		x, err := numberOfPods(ctx)
		if err != nil {
			return false, err
		}
		// We want exactly 2. Not 1, not panicing 3, just 2.
		return x == 2, nil
	}); err != nil {
		t.Fatalf("Desired scale of 2 never achieved: %v", err)
	}

	// Now read the service endpoints and make sure there are 2 endpoints there.
	// We poll, since network programming takes times, but the timeout is set for
	// uniformness with one above.
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		svcEps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(
			ctx.resources.Revision.Status.ServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		t.Logf("resources.ReadyAddressCount(svcEps) = %d", resources.ReadyAddressCount(svcEps))
		return resources.ReadyAddressCount(svcEps) == 2, nil
	}); err != nil {
		t.Errorf("Never achieved subset of size 2: %v", err)
	}
}

func TestTargetBurstCapacityMinusOne(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, 10 /* target concurrency*/, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}))
	defer test.TearDown(ctx.clients, ctx.names)

	_, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatalf("Error retrieving autoscaler configmap: %v", err)
	}
	aeps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(
		system.Namespace()).Get(networking.ActivatorServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error getting activator endpoints: %v", err)
	}
	t.Logf("Activator endpoints: %v", aeps)

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(ctx.resources, ctx.clients); err != nil {
		t.Fatalf("Never got Activator endpoints in the service: %v", err)
	}
}

func TestFastScaleToZero(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(),
		}))
	defer test.TearDown(ctx.clients, ctx.names)

	cfg, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatalf("Error retrieving autoscaler configmap: %v", err)
	}

	epsL, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s",
			serving.RevisionLabelKey, ctx.resources.Revision.Name,
			networking.ServiceTypeKey, networking.ServiceTypePrivate,
		),
	})
	if err != nil || len(epsL.Items) == 0 {
		t.Fatalf("No endpoints or error: %v", err)
	}

	epsN := epsL.Items[0].Name
	t.Logf("Waiting for emptying of %q ", epsN)

	// The first thing that happens when pods are starting to terminate,
	// if that they stop being ready and endpoints controller removes them
	// from the ready set.
	// While pod termination itself can last quite some time (our pod termination
	// test allows for up to a minute). The 15s delay is based upon maximum
	// of 20 runs (11s) + 4s of buffer for reliability.
	st := time.Now()
	if err := wait.PollImmediate(1*time.Second, cfg.ScaleToZeroGracePeriod+15*time.Second, func() (bool, error) {
		eps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(epsN, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return resources.ReadyAddressCount(eps) == 0, nil
	}); err != nil {
		t.Fatalf("Did not observe %q to actually be emptied", epsN)
	}

	t.Logf("Total time to scale down: %v", time.Since(st))
}
