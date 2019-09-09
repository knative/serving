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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	autoscaleExpectedOutput = "399989"
	// Concurrency must be high enough to avoid the problems with sampling
	// but not high enough to generate scheduling problems.
	containerConcurrency = 6
	targetUtilization    = 0.7
)

type testContext struct {
	t                 *testing.T
	clients           *test.Clients
	names             test.ResourceNames
	resources         *v1a1test.ResourceObjects
	deploymentName    string
	domain            string
	targetUtilization float64
	targetValue       int
	metric            string
}

func getVegetaTarget(kubeClientset *kubernetes.Clientset, domain, endpointOverride string, resolvable bool) (vegeta.Target, error) {
	if resolvable {
		return vegeta.Target{
			Method: "GET",
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
		Method: "GET",
		URL:    fmt.Sprintf("http://%s?sleep=100", endpoint),
		Header: h,
	}, nil
}

// TODO: use vegeta to send traffic for this func as well.
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
						ctx.t.Logf("Status = %d, want: 200", res.StatusCode)
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
		return fmt.Errorf("error making requests for scale up. total = %d, errors = %d, expected 0",
			totalRequests, totalRequests-successfulRequests)
	}
	return nil
}

func generateTrafficAtFixedRPS(ctx *testContext, rps int, duration time.Duration, stopChan chan struct{}) error {
	var (
		totalRequests      int32
		successfulRequests int32
	)

	rate := vegeta.Rate{Freq: rps, Per: time.Second}
	target, err := getVegetaTarget(
		ctx.clients.KubeClient.Kube, ctx.domain, pkgTest.Flags.IngressEndpoint, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return fmt.Errorf("error creating vegeta target: %v", err)
	}
	targeter := vegeta.NewStaticTargeter(target)
	attacker := vegeta.NewAttacker(vegeta.Timeout(duration))

	ctx.t.Logf("Maintaining %v RPS requests for %v.", rps, duration)

	// Start the attack!
	results := attacker.Attack(targeter, rate, duration, "load-test")
	defer attacker.Stop()

	for {
		select {
		case <-stopChan:
			ctx.t.Log("Stopping generateTraffic")
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

	if successfulRequests != totalRequests {
		return fmt.Errorf("error making requests for scale up. total = %d, errors = %d, expected 0",
			totalRequests, totalRequests-successfulRequests)
	}
	return nil
}

// setup creates a new service, with given service options.
// It returns a testContext that has resources, K8s clients,
// and the deployment.
// It sets up CleanupOnInterrupt as well that will destroy the resources
// when the test terminates.
func setup(t *testing.T, class, metric string, target int, targetUtilization float64, fopts ...rtesting.ServiceOption) *testContext {
	t.Helper()
	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "autoscale",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		append(fopts, rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.ClassAnnotationKey:             class,
			autoscaling.MetricAnnotationKey:            metric,
			autoscaling.TargetAnnotationKey:            strconv.FormatFloat(float64(target), 'f', -1, 64),
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

	domain := resources.Route.Status.URL.Host
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		// Istio doesn't expose a status for us here: https://github.com/istio/istio/issues/6082
		// TODO(tcnghia): Remove this when https://github.com/istio/istio/issues/882 is fixed.
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK)),
		"CheckingEndpointAfterUpdating",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing domain %s: %v", domain, err)
	}

	return &testContext{
		t:                 t,
		clients:           clients,
		names:             names,
		resources:         resources,
		deploymentName:    resourcenames.Deployment(resources.Revision),
		domain:            domain,
		targetUtilization: targetUtilization,
		targetValue:       target,
		metric:            metric,
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
	ctx.t.Helper()
	// There are two test modes: quick, and not quick.
	// 1) Quick mode: succeeds when the number of pods meets targetPods.
	// 2) Not Quick (sustaining) mode: succeeds when the number of pods gets scaled to targetPods and
	//    sustains there for the `duration`.

	// Relax the bounds to reduce the flakiness caused by sampling in the autoscaling algorithm.
	// Also adjust the values by the target utilization values.

	minPods := int32(math.Floor(float64(curPods)/ctx.targetUtilization)) - 1
	maxPods := int32(math.Ceil(float64(targetPods)/ctx.targetUtilization)) + 1

	stopChan := make(chan struct{})
	var grp errgroup.Group
	switch ctx.metric {
	case autoscaling.RPS:
		grp.Go(func() error {
			return generateTrafficAtFixedRPS(ctx, int(targetPods)*ctx.targetValue, duration, stopChan)
		})
	default:
		grp.Go(func() error {
			return generateTraffic(ctx, int(targetPods)*ctx.targetValue, duration, stopChan)
		})
	}

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
			cancel := logstream.Start(t)
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
		return generateTraffic(ctx, 7, duration, stopCh)
	})

	// Wait for the endpoints to equalize.
	// There's some jitter in the beginning possible, since it takes 2 seconds
	// for us to start evaluating things.
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		aeps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(system.Namespace()).Get(networking.ActivatorServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		svcEps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(
			ctx.resources.Revision.Status.ServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return cmp.Equal(svcEps.Subsets, aeps.Subsets), nil
	}); err != nil {
		t.Fatalf("Initial state never achieved: %v", err)
	}

	// Start second load generator.
	grp.Go(func() error {
		return generateTraffic(ctx, 5, duration, stopCh)
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

	cfg, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatalf("Error retrieving autoscaler configmap: %v", err)
	}
	aeps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(
		system.Namespace()).Get(networking.ActivatorServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error getting activator endpoints: %v", err)
	}
	t.Logf("Activator endpoints: %v", aeps)

	// Wait for the endpoints to equalize.
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		svcEps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(
			ctx.resources.Revision.Status.ServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return cmp.Equal(svcEps.Subsets, aeps.Subsets), nil
	}); err != nil {
		t.Fatalf("Initial state never achieved: %v", err)
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
