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
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"knative.dev/pkg/test/logging"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/ingress"
	"knative.dev/pkg/test/spoof"
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
	rpsTarget            = 10
	targetUtilization    = 0.7
	successRateSLO       = 0.999
	autoscaleSleep       = 500

	autoscaleTestImageName = "autoscale"
)

// TestContext includes context for autoscaler testing.
type TestContext struct {
	t                 *testing.T
	logf              logging.FormatLogger
	clients           *test.Clients
	names             *test.ResourceNames
	resources         *v1test.ResourceObjects
	targetUtilization float64
	targetValue       int
	metric            string
}

// Clients returns the clients of the TestContext.
func (ctx *TestContext) Clients() *test.Clients {
	return ctx.clients
}

// Resources returns the resources of the TestContext.
func (ctx *TestContext) Resources() *v1test.ResourceObjects {
	return ctx.resources
}

// SetResources sets the resources of the TestContext to the given values.
func (ctx *TestContext) SetResources(resources *v1test.ResourceObjects) {
	ctx.resources = resources
}

// Names returns the resource names of the TestContext.
func (ctx *TestContext) Names() *test.ResourceNames {
	return ctx.names
}

// SetNames set the resource names of the TestContext to the given values.
func (ctx *TestContext) SetNames(names *test.ResourceNames) {
	ctx.names = names
}

// SetLogger sets the logger of the TestContext.
func (ctx *TestContext) SetLogger(logf logging.FormatLogger) {
	ctx.logf = logf
}

func getVegetaTarget(kubeClientset kubernetes.Interface, domain, endpointOverride string, resolvable bool) (vegeta.Target, error) {
	if resolvable {
		return vegeta.Target{
			Method: http.MethodGet,
			URL:    fmt.Sprintf("http://%s?sleep=%d", domain, autoscaleSleep),
		}, nil
	}

	// If the domain that the Route controller is configured to assign to Route.Status.Domain
	// (the domainSuffix) is not resolvable, we need to retrieve the endpoint and spoof
	// the Host in our requests.
	endpoint, mapper, err := ingress.GetIngressEndpoint(context.Background(), kubeClientset, endpointOverride)
	if err != nil {
		return vegeta.Target{}, err
	}

	h := http.Header{}
	h.Set("Host", domain)
	return vegeta.Target{
		Method: http.MethodGet,
		URL:    fmt.Sprintf("http://%s:%s?sleep=%d", endpoint, mapper("80"), autoscaleSleep),
		Header: h,
	}, nil
}

func generateTraffic(
	ctx *TestContext,
	attacker *vegeta.Attacker,
	pacer vegeta.Pacer,
	stopChan chan struct{}) error {

	target, err := getVegetaTarget(
		ctx.clients.KubeClient, ctx.resources.Route.Status.URL.URL().Hostname(), pkgTest.Flags.IngressEndpoint, test.ServingFlags.ResolvableDomain)
	if err != nil {
		return fmt.Errorf("error creating vegeta target: %w", err)
	}

	// The 0 duration means that the attack will only be controlled by the `Stop` function.
	results := attacker.Attack(vegeta.NewStaticTargeter(target), pacer, 0, "load-test")
	defer attacker.Stop()

	var (
		totalRequests      int32
		successfulRequests int32
	)
	for {
		select {
		case <-stopChan:
			ctx.logf("Stopping generateTraffic")
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
				ctx.logf("Time is up; done")
				return nil
			}

			totalRequests++
			if res.Code != http.StatusOK {
				ctx.logf("Status = %d, want: 200", res.Code)
				ctx.logf("URL: %s Duration: %v Error: %s Body:\n%s", res.URL, res.Latency, res.Error, string(res.Body))
				continue
			}
			successfulRequests++
		}
	}
}

func generateTrafficAtFixedConcurrency(ctx *TestContext, concurrency int, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{} // Sends requests as quickly as possible, capped by MaxWorkers below.
	attacker := vegeta.NewAttacker(
		vegeta.Timeout(0), // No timeout is enforced at all.
		vegeta.Workers(uint64(concurrency)),
		vegeta.MaxWorkers(uint64(concurrency)))

	ctx.logf("Maintaining %d concurrent requests.", concurrency)
	return generateTraffic(ctx, attacker, pacer, stopChan)
}

func generateTrafficAtFixedRPS(ctx *TestContext, rps int, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{Freq: rps, Per: time.Second}
	attacker := vegeta.NewAttacker(vegeta.Timeout(0)) // No timeout is enforced at all.

	ctx.logf("Maintaining %v RPS.", rps)
	return generateTraffic(ctx, attacker, pacer, stopChan)
}

func toPercentageString(f float64) string {
	return strconv.FormatFloat(f*100, 'f', -1, 64)
}

// SetupSvc creates a new service, with given service options.
// It returns a TestContext that has resources, K8s clients and other needed
// data points.
// It sets up EnsureTearDown to ensure that resources are cleaned up when the
// test terminates.
func SetupSvc(t *testing.T, class, metric string, target int, targetUtilization float64, fopts ...rtesting.ServiceOption) *TestContext {
	t.Helper()
	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names := &test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   autoscaleTestImageName,
	}
	resources, err := v1test.CreateServiceReady(t, clients, names,
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
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
			}),
		}, fopts...)...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		v1test.RetryingRouteInconsistency(spoof.MatchesAllOf(spoof.IsStatusOK)),
		"CheckingEndpointAfterCreate",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", names.URL.Hostname(), err)
	}

	return &TestContext{
		t:                 t,
		logf:              t.Logf,
		clients:           clients,
		names:             names,
		resources:         resources,
		targetUtilization: targetUtilization,
		targetValue:       target,
		metric:            metric,
	}
}

func assertScaleDown(ctx *TestContext) {
	deploymentName := resourcenames.Deployment(ctx.resources.Revision)
	if err := WaitForScaleToZero(ctx.t, deploymentName, ctx.clients); err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	ctx.t.Logf("Wait for all pods to terminate.")

	if err := pkgTest.WaitForPodListState(
		context.Background(),
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

	ctx.t.Logf("The Revision should remain ready after scaling to zero.")
	if err := v1test.CheckRevisionState(ctx.clients.ServingClient, ctx.names.Revision, v1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to zero: %v", ctx.names.Revision, err)
	}

	ctx.t.Logf("Scaled down.")
}

func numberOfReadyPods(ctx *TestContext) (float64, error) {
	// SKS name matches that of revision.
	n := ctx.resources.Revision.Name
	sks, err := ctx.clients.NetworkingClient.ServerlessServices.Get(context.Background(), n, metav1.GetOptions{})
	if err != nil {
		ctx.logf("Error getting SKS %q: %v", n, err)
		return 0, fmt.Errorf("error retrieving sks %q: %w", n, err)
	}
	if sks.Status.PrivateServiceName == "" {
		ctx.logf("SKS %s has not yet reconciled", n)
		// Not an error, but no pods either.
		return 0, nil
	}
	eps, err := ctx.clients.KubeClient.CoreV1().Endpoints(test.ServingNamespace).Get(
		context.Background(), sks.Status.PrivateServiceName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get endpoints %s: %w", sks.Status.PrivateServiceName, err)
	}
	return float64(resources.ReadyAddressCount(eps)), nil
}

func checkPodScale(ctx *TestContext, targetPods, minPods, maxPods float64, done <-chan time.Time, quick bool) error {
	// Short-circuit traffic generation once we exit from the check logic.
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
			ctx.logf(mes)
			// verify that the number of pods doesn't go down while we are scaling up.
			if got < minPods {
				return errors.New("interim scale didn't fulfill constraints: " + mes)
			}
			// A quick test succeeds when the number of pods scales up to `targetPods`
			// (and, as an extra check, no more than `maxPods`).
			if quick && got >= targetPods && got <= maxPods {
				ctx.logf("Quick Mode: got %v >= %v", got, targetPods)
				return nil
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
			ctx.logf(mes)
			if got < targetPods-1 || got > maxPods {
				return errors.New("final scale didn't fulfill constraints: " + mes)
			}
			return nil
		}
	}
}

// AssertAutoscaleUpToNumPods asserts the number of pods gets scaled to targetPods.
// It supports two test modes: quick, and not quick.
// 1) Quick mode: succeeds when the number of pods meets targetPods.
// 2) Not Quick (sustaining) mode: succeeds when the number of pods gets scaled to targetPods and
//    sustains there until the `done` channel sends a signal.
// The given `duration` is how long the traffic will be generated. You must make sure that the signal
// from the given `done` channel will be sent within the `duration`.
func AssertAutoscaleUpToNumPods(ctx *TestContext, curPods, targetPods float64, done <-chan time.Time, quick bool) {
	grp := AutoscaleUpToNumPods(ctx, curPods, targetPods, done, quick)
	if err := grp.Wait(); err != nil {
		ctx.t.Fatal(err)
	}
}

// AutoscaleUpToNumPods starts the traffic for AssertAutoscaleUpToNumPods and returns
// an error group to wait for. Starting the routines is separated from waiting for
// easy re-use in other places (e.g. upgrade tests).
func AutoscaleUpToNumPods(ctx *TestContext, curPods, targetPods float64, done <-chan time.Time, quick bool) *errgroup.Group {
	// Relax the bounds to reduce the flakiness caused by sampling in the autoscaling algorithm.
	// Also adjust the values by the target utilization values.
	minPods := math.Floor(curPods/ctx.targetUtilization) - 1
	maxPods := math.Ceil(math.Ceil(targetPods/ctx.targetUtilization) * 1.1)

	stopChan := make(chan struct{})
	grp := &errgroup.Group{}
	grp.Go(func() error {
		switch ctx.metric {
		case autoscaling.RPS:
			return generateTrafficAtFixedRPS(ctx, int(targetPods*float64(ctx.targetValue)), stopChan)
		default:
			return generateTrafficAtFixedConcurrency(ctx, int(targetPods*float64(ctx.targetValue)), stopChan)
		}
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, done, quick)
	})

	return grp
}
