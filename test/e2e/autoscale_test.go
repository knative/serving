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
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	autoscaleExpectedOutput = "399989"
)

func isDeploymentScaledUp() func(d *appsv1.Deployment) (bool, error) {
	return func(d *appsv1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas > 1, nil
	}
}

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
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", ctx.domain), nil)
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

func setup(t *testing.T) *testContext {
	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "autoscale",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{
		ContainerConcurrency: 10,
	})
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
		deploymentName: resources.Revision.Name,
		domain:         domain,
	}
}

func assertScaleUp(ctx *testContext) {
	ctx.t.Log("The autoscaler spins up additional replicas when traffic increases.")
	if err := generateTraffic(ctx, 20, 20*time.Second, nil); err != nil {
		ctx.t.Fatalf("Error during initial scale up: %v", err)
	}
	ctx.t.Logf("Waiting for scale up revision %s", ctx.names.Revision)
	if err := pkgTest.WaitForDeploymentState(
		ctx.clients.KubeClient,
		ctx.deploymentName,
		isDeploymentScaledUp(),
		"DeploymentIsScaledUp",
		test.ServingNamespace,
		2*time.Minute); err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling up. %s", ctx.deploymentName, err)
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

func TestAutoscaleUpDownUp(t *testing.T) {
	t.Parallel()
	ctx := setup(t)
	defer test.TearDown(ctx.clients, ctx.names)

	sc := startDiagnosis(t, ctx.clients, 15*time.Second)
	defer sc.stop()

	assertScaleUp(ctx)
	assertScaleDown(ctx)
	assertScaleUp(ctx)
}

func assertNumberOfPods(ctx *testContext, numReplicasMin int32, numReplicasMax int32) error {
	deployment, err := ctx.clients.KubeClient.Kube.AppsV1().Deployments(test.ServingNamespace).Get(ctx.deploymentName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "Failed to get deployment %q", deployment)
	}
	gotReplicas := deployment.Status.ReadyReplicas
	mes := fmt.Sprintf("got %d replicas, expected between [%d, %d] replicas for deployment %s", gotReplicas, numReplicasMin, numReplicasMax, ctx.deploymentName)
	ctx.t.Log(mes)
	if gotReplicas < numReplicasMin || gotReplicas > numReplicasMax {
		return errors.New(mes)
	}
	return nil
}

func assertAutoscaleUpToNumPods(ctx *testContext, numPods int32) {
	// Relaxing the pod count requirement a little bit to avoid being too flaky.
	minPods := numPods - 1
	maxPods := numPods + 1

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		return generateTraffic(ctx, int(numPods*10), 60*time.Second, stopChan)
	})
	grp.Go(func() error {
		// Short-circuit traffic generation once we exit from the check logic.
		defer close(stopChan)

		timer := time.Tick(2 * time.Second)
		for {
			select {
			case <-timer:
				if err := assertNumberOfPods(ctx, minPods, maxPods); err != nil {
					return err
				}
				if err := assertNumberOfPods(ctx, numPods, maxPods); err == nil {
					return nil
				}
			}
		}
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Error(err)
	}
}

func TestAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()
	ctx := setup(t)
	defer test.TearDown(ctx.clients, ctx.names)

	ctx.t.Log("The autoscaler spins up additional replicas when traffic increases.")
	// note: without the warm-up / gradual increase of load the test is retrieving a 503 (overload) from the envoy

	// Increase workload for 2 replicas for 60s
	// Assert the number of expected replicas is between n-1 and n+1, where n is the # of desired replicas for 60s.
	// Assert the number of expected replicas is n and n+1 at the end of 60s, where n is the # of desired replicas.
	assertAutoscaleUpToNumPods(ctx, 2)
	// Increase workload scale to 3 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	assertAutoscaleUpToNumPods(ctx, 3)
	// Increase workload scale to 4 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	assertAutoscaleUpToNumPods(ctx, 4)

}
