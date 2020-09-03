/*
Copyright 2020 The Knative Authors

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
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/ingress"
	"knative.dev/serving/pkg/apis/autoscaling"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	// Concurrency must be high enough to avoid the problems with sampling
	// but not high enough to generate scheduling problems.
	containerConcurrency = 6
	targetUtilization    = 0.7
	successRateSLO       = 0.999
	autoscaleSleep       = 500

	autoscaleTestImageName = "autoscale"
)

type testContext struct {
	t                 *testing.T
	clients           *test.Clients
	names             test.ResourceNames
	resources         *v1test.ResourceObjects
	targetUtilization float64
	targetValue       int
	metric            string
}

func getVegetaTarget(kubeClientset *kubernetes.Clientset, domain, endpointOverride string, resolvable bool) (vegeta.Target, error) {
	if resolvable {
		return vegeta.Target{
			Method: http.MethodGet,
			URL:    fmt.Sprintf("http://%s?sleep=%d", domain, autoscaleSleep),
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
		URL:    fmt.Sprintf("http://%s?sleep=%d", endpoint, autoscaleSleep),
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
				ctx.t.Logf("URL: %s Duration: %v Body:\n%s", res.URL, res.Latency, string(res.Body))
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

func validateEndpoint(t *testing.T, clients *test.Clients, names test.ResourceNames) error {
	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		names.URL,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK)),
		"CheckingEndpointAfterUpdating",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
	)
	return err
}

func toPercentageString(f float64) string {
	return strconv.FormatFloat(f*100, 'f', -1, 64)
}

// setup creates a new service, with given service options.
// It returns a testContext that has resources, K8s clients and other needed
// data points.
// It sets up EnsureTearDown to ensure that resources are cleaned up when the
// test terminates.
func setup(t *testing.T, class, metric string, target int, targetUtilization float64, fopts ...rtesting.ServiceOption) *testContext {
	t.Helper()
	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   autoscaleTestImageName,
	}
	test.EnsureTearDown(t, clients, &names)
	resources, err := v1test.CreateServiceReady(t, clients, &names,
		append([]rtesting.ServiceOption{
			rtesting.WithConfigAnnotations(map[string]string{
				autoscaling.ClassAnnotationKey:             class,
				autoscaling.MetricAnnotationKey:            metric,
				autoscaling.TargetAnnotationKey:            strconv.Itoa(target),
				autoscaling.TargetUtilizationPercentageKey: toPercentageString(targetUtilization),
				// We run the test for 60s, so make window a bit shorter,
				// so that we're operating in sustained mode and the pod actions stopped happening.
				autoscaling.WindowAnnotationKey: "50s",
			}), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("300Mi"),
				},
			}),
		}, fopts...)...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if err := validateEndpoint(t, clients, names); err != nil {
		t.Fatalf("Error probing %s: %v", names.URL.Hostname(), err)
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
			for i := range p.Items {
				pod := &p.Items[i]
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
	if err := v1test.CheckRevisionState(ctx.clients.ServingClient, ctx.names.Revision, v1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to zero: %v", ctx.names.Revision, err)
	}

	ctx.t.Log("Scaled down.")
}

func numberOfReadyPods(ctx *testContext) (float64, error) {
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

func checkPodScale(ctx *testContext, targetPods, minPods, maxPods float64, duration time.Duration, quick bool) error {
	// Short-circuit traffic generation once we exit from the check logic.
	done := time.After(duration)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Each 2 second, check that the number of pods is at least `minPods`. `minPods` is increasing
			// to verify that the number of pods doesn't go down while we are scaling up.
			got, err := numberOfReadyPods(ctx)
			if err != nil {
				return err
			}
			mes := fmt.Sprintf("revision %q #replicas: %v, want at least: %v", ctx.resources.Revision.Name, got, minPods)
			ctx.t.Log(mes)
			// verify that the number of pods doesn't go down while we are scaling up.
			if got < minPods {
				return errors.New("interim scale didn't fulfill constraints: " + mes)
			}
			// A quick test succeeds when the number of pods scales up to `targetPods`
			// (and, for sanity check, no more than `maxPods`).
			if got >= targetPods && got <= maxPods {
				if quick {
					// A quick test succeeds when the number of pods scales up to `targetPods`
					// (and, for sanity check, no more than `maxPods`).
					if got >= targetPods && got <= maxPods {
						ctx.t.Logf("Quick Mode: got %v >= %v", got, targetPods)
						return nil
					}
				}
			}
			if minPods < targetPods-1 {
				// Increase `minPods`, but leave room to reduce flakiness.
				minPods = math.Min(got, targetPods) - 1
			}

		case <-done:
			// The test duration is over. Do a last check to verify that the number of pods is at `targetPods`
			// (with a little room for de-flakiness).
			got, err := numberOfReadyPods(ctx)
			if err != nil {
				return fmt.Errorf("failed to fetch number of ready pods: %w", err)
			}
			mes := fmt.Sprintf("got %v replicas, expected between [%v, %v] replicas for revision %s",
				got, targetPods-1, maxPods, ctx.resources.Revision.Name)
			ctx.t.Log(mes)
			if got < targetPods-1 || got > maxPods {
				return errors.New("final scale didn't fulfill constraints: " + mes)
			}
			return nil
		}
	}
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
			return generateTrafficAtFixedRPS(ctx, int(targetPods*float64(ctx.targetValue)), duration, stopChan)
		default:
			return generateTrafficAtFixedConcurrency(ctx, int(targetPods*float64(ctx.targetValue)), duration, stopChan)
		}
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, duration, quick)
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Errorf("Error: %v", err)
	}
}

// RunAutoscaleUpCountPods is a test kernel to test the chosen autoscaler using the given
// metric tracks the given target.
func RunAutoscaleUpCountPods(t *testing.T, class, metric string) {
	target := containerConcurrency
	if metric == autoscaling.RPS {
		target = 10
	}

	ctx := setup(t, class, metric, target, targetUtilization)

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
}
