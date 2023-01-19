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
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	vegeta "github.com/tsenart/vegeta/v12/lib"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/ingress"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
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
	t          *testing.T
	clients    *test.Clients
	names      *test.ResourceNames
	resources  *v1test.ResourceObjects
	autoscaler *AutoscalerOptions
}

// AutoscalerOptions holds autoscaling parameters for knative service.
type AutoscalerOptions struct {
	Class             string
	Metric            string
	TargetUtilization float64
	Target            int
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

func getVegetaTarget(kubeClientset kubernetes.Interface, domain, endpointOverride string, resolvable bool, paramName string, paramValue int) (vegeta.Target, error) {
	if resolvable {
		return vegeta.Target{
			Method: http.MethodGet,
			URL:    fmt.Sprintf("http://%s?%s=%d", domain, paramName, paramValue),
		}, nil
	}

	// If the domain that the Route controller is configured to assign to Route.Status.Domain
	// (the domainSuffix) is not resolvable, we need to retrieve the endpoint and spoof
	// the Host in our requests.
	endpoint, mapper, err := ingress.GetIngressEndpoint(context.Background(), kubeClientset, endpointOverride)
	if err != nil {
		return vegeta.Target{}, err
	}

	return vegeta.Target{
		Method: http.MethodGet,
		URL:    fmt.Sprintf("http://%s:%s?%s=%d", endpoint, mapper("80"), paramName, paramValue),
		Header: http.Header{"Host": []string{domain}},
	}, nil
}

func generateTraffic(
	ctx *TestContext,
	attacker *vegeta.Attacker,
	pacer vegeta.Pacer,
	stopChan chan struct{},
	target vegeta.Target) error {

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
			ctx.t.Logf("Stopping generateTraffic")
			successRate := float64(1)
			if totalRequests > 0 {
				successRate = float64(successfulRequests) / float64(totalRequests)
			}
			if successRate < successRateSLO {
				return fmt.Errorf("request success rate under SLO: total = %d, errors = %d, rate = %f, SLO = %f",
					totalRequests, totalRequests-successfulRequests, successRate, successRateSLO)
			}
			return nil
		case res := <-results:
			totalRequests++
			if res.Code != http.StatusOK {
				ctx.t.Logf("Status = %d, want: 200", res.Code)
				ctx.t.Logf("URL: %s Start: %s End: %s Duration: %v Error: %s Body:\n%s",
					res.URL, res.Timestamp.Format(time.RFC3339), res.End().Format(time.RFC3339), res.Latency, res.Error, string(res.Body))
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
	target, err := getVegetaTarget(
		ctx.clients.KubeClient, ctx.resources.Route.Status.URL.URL().Hostname(), pkgTest.Flags.IngressEndpoint, test.ServingFlags.ResolvableDomain, "sleep", autoscaleSleep)
	if err != nil {
		return fmt.Errorf("error creating vegeta target: %w", err)
	}

	ctx.t.Logf("Maintaining %d concurrent requests.", concurrency)
	return generateTraffic(ctx, attacker, pacer, stopChan, target)
}

func generateTrafficAtFixedRPS(ctx *TestContext, rps int, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{Freq: rps, Per: time.Second}
	attacker := vegeta.NewAttacker(vegeta.Timeout(0)) // No timeout is enforced at all.
	target, err := getVegetaTarget(
		ctx.clients.KubeClient, ctx.resources.Route.Status.URL.URL().Hostname(), pkgTest.Flags.IngressEndpoint, test.ServingFlags.ResolvableDomain, "sleep", autoscaleSleep)
	if err != nil {
		return fmt.Errorf("error creating vegeta target: %w", err)
	}

	ctx.t.Logf("Maintaining %v RPS.", rps)
	return generateTraffic(ctx, attacker, pacer, stopChan, target)
}

func toPercentageString(f float64) string {
	return strconv.FormatFloat(f*100, 'f', -1, 64)
}

// SetupSvc creates a new service, with given service options.
// It returns a TestContext that has resources, K8s clients and other needed
// data points.
func SetupSvc(t *testing.T, aopts *AutoscalerOptions, topts test.Options, fopts ...rtesting.ServiceOption) *TestContext {
	t.Helper()
	clients := test.Setup(t, topts)

	t.Log("Creating a new Route and Configuration")
	names := &test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   autoscaleTestImageName,
	}
	resources, err := v1test.CreateServiceReady(t, clients, names,
		append([]rtesting.ServiceOption{
			rtesting.WithConfigAnnotations(map[string]string{
				autoscaling.ClassAnnotationKey:             aopts.Class,
				autoscaling.MetricAnnotationKey:            aopts.Metric,
				autoscaling.TargetAnnotationKey:            strconv.Itoa(aopts.Target),
				autoscaling.TargetUtilizationPercentageKey: toPercentageString(aopts.TargetUtilization),
				// Reduce the amount of historical data we need before scaling down to account for
				// the fact that the chaosduck will only let a bucket leader live for ~30s.  This
				// value still allows the chaosduck to make us failover, but is low enough that we
				// should not need to survive multiple rounds of chaos in order to scale a
				// revision down.
				autoscaling.WindowAnnotationKey: "30s",
			}),
			rtesting.WithResourceRequirements(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
			}),
		}, fopts...)...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		spoof.MatchesAllOf(spoof.IsStatusOK),
		"CheckingEndpointAfterCreate",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", names.URL.Hostname(), err)
	}

	return &TestContext{
		t:          t,
		clients:    clients,
		names:      names,
		resources:  resources,
		autoscaler: aopts,
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
		"WaitForAvailablePods", test.ServingFlags.TestNamespace); err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods of %q: %v", deploymentName, err)
	}

	ctx.t.Logf("The Revision should remain ready after scaling to zero.")
	if err := v1test.CheckRevisionState(ctx.clients.ServingClient, ctx.names.Revision, v1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to zero: %v", ctx.names.Revision, err)
	}

	ctx.t.Logf("Scaled down.")
}

func numberOfReadyPods(ctx *TestContext) (float64, *appsv1.Deployment, error) {
	n := resourcenames.Deployment(ctx.resources.Revision)
	deploy, err := ctx.clients.KubeClient.AppsV1().Deployments(test.ServingFlags.TestNamespace).Get(
		context.Background(), n, metav1.GetOptions{})
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get deployment %s: %w", n, err)
	}

	if isInRollout(deploy) {
		// Ref: #11092
		// The deployment was updated and the update is being rolled out so we defensively
		// pick the desired replicas to assert the autoscaling decisions.
		// TODO: Drop this once we solved the underscale issue.
		ctx.t.Logf("Deployment is being rolled, picking spec.replicas=%d", *deploy.Spec.Replicas)
		return float64(*deploy.Spec.Replicas), deploy, nil
	}
	// Otherwise we pick the ready pods to assert maximum consistency for ramp up tests.
	return float64(deploy.Status.ReadyReplicas), deploy, nil
}

func checkPodScale(ctx *TestContext, targetPods, minPods, maxPods float64, done <-chan time.Time, quick bool) error {
	// Short-circuit traffic generation once we exit from the check logic.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	originalMaxPods := maxPods
	for {
		select {
		case <-ticker.C:
			// Each 2 second, check that the number of pods is at least `minPods`. `minPods` is increasing
			// to verify that the number of pods doesn't go down while we are scaling up.
			got, d, err := numberOfReadyPods(ctx)
			if err != nil {
				return err
			}

			if isInRollout(d) {
				// Ref: #11092
				// Allow for a higher scale if the deployment is being rolled as that
				// might be skewing metrics in the autoscaler.
				maxPods = math.Ceil(originalMaxPods * 1.2)
			}

			mes := fmt.Sprintf("revision %q #replicas: %v, want at least: %v", ctx.resources.Revision.Name, got, minPods)
			ctx.t.Logf(mes)
			// verify that the number of pods doesn't go down while we are scaling up.
			if got < minPods {
				return fmt.Errorf("interim scale didn't fulfill constraints: %s\ndeployment state: %s", mes, spew.Sdump(d))
			}
			// A quick test succeeds when the number of pods scales up to `targetPods`
			// (and, as an extra check, no more than `maxPods`).
			if quick && got >= targetPods && got <= maxPods {
				ctx.t.Logf("Quick Mode: got %v >= %v", got, targetPods)
				return nil
			}
			if minPods < targetPods-1 {
				// Increase `minPods`, but leave room to reduce flakiness.
				minPods = math.Min(got, targetPods) - 1
			}

		case <-done:
			// The test duration is over. Do a last check to verify that the number of pods is at `targetPods`
			// (with a little room for de-flakiness).
			got, d, err := numberOfReadyPods(ctx)
			if err != nil {
				return fmt.Errorf("failed to fetch number of ready pods: %w", err)
			}

			if isInRollout(d) {
				// Ref: #11092
				// Allow for a higher scale if the deployment is being rolled as that
				// might be skewing metrics in the autoscaler.
				maxPods = math.Ceil(originalMaxPods * 1.2)
			}

			mes := fmt.Sprintf("revision %q #replicas: %v, want between [%v, %v]", ctx.resources.Revision.Name, got, targetPods-1, maxPods)
			ctx.t.Logf(mes)
			if got < targetPods-1 || got > maxPods {
				return fmt.Errorf("final scale didn't fulfill constraints: %s\ndeployment state: %s", mes, spew.Sdump(d))
			}
			return nil
		}
	}
}

// AssertAutoscaleUpToNumPods asserts the number of pods gets scaled to targetPods.
// It supports two test modes: quick, and not quick.
//  1. Quick mode: succeeds when the number of pods meets targetPods.
//  2. Not Quick (sustaining) mode: succeeds when the number of pods gets scaled to targetPods and
//     sustains there until the `done` channel sends a signal.
func AssertAutoscaleUpToNumPods(ctx *TestContext, curPods, targetPods float64, done <-chan time.Time, quick bool) {
	ctx.t.Helper()
	wait := AutoscaleUpToNumPods(ctx, curPods, targetPods, done, quick)
	if err := wait(); err != nil {
		ctx.t.Fatal(err)
	}
}

// AutoscaleUpToNumPods starts the traffic for AssertAutoscaleUpToNumPods and returns
// a function to wait for which will return any error from test execution.
// Starting the routines is separated from waiting for easy re-use in other
// places (e.g. upgrade tests).
func AutoscaleUpToNumPods(ctx *TestContext, curPods, targetPods float64, done <-chan time.Time, quick bool) func() error {
	ctx.t.Helper()
	// Relax the bounds to reduce the flakiness caused by sampling in the autoscaling algorithm.
	// Also adjust the values by the target utilization values.
	minPods := math.Floor(curPods/ctx.autoscaler.TargetUtilization) - 1
	maxPods := math.Ceil(math.Ceil(targetPods/ctx.autoscaler.TargetUtilization) * 1.1)

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		switch ctx.autoscaler.Metric {
		case autoscaling.RPS:
			return generateTrafficAtFixedRPS(ctx, int(targetPods*float64(ctx.autoscaler.Target)), stopChan)
		default:
			return generateTrafficAtFixedConcurrency(ctx, int(targetPods*float64(ctx.autoscaler.Target)), stopChan)
		}
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, done, quick)
	})

	return grp.Wait
}

// isInRollout is a loose copy of the kubectl function handling rollouts.
// See: https://github.com/kubernetes/kubectl/blob/0149779a03735a5d483115ca4220a7b6c861430c/pkg/polymorphichelpers/rollout_status.go#L75-L91
func isInRollout(deploy *appsv1.Deployment) bool {
	if deploy.Generation > deploy.Status.ObservedGeneration {
		// Waiting for update to be observed.
		return true
	}
	if deploy.Spec.Replicas != nil && deploy.Status.UpdatedReplicas < *deploy.Spec.Replicas {
		// Not enough replicas updated yet.
		return true
	}
	if deploy.Status.Replicas > deploy.Status.UpdatedReplicas {
		// Old replicas are being terminated.
		return true
	}
	if deploy.Status.AvailableReplicas < deploy.Status.UpdatedReplicas {
		// Not enough available yet.
		return true
	}
	return false
}
