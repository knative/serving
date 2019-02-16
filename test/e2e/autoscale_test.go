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
	"sync"
	"testing"
	"time"

	_ "github.com/knative/pkg/system/testing"
	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/test"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	autoscaleExpectedOutput = "399989"
)

func isDeploymentScaledUp() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas > 1, nil
	}
}

func tearDown(ctx *testContext) {
	test.TearDown(ctx.clients, ctx.names)
}

func generateTraffic(ctx *testContext, concurrency int, duration time.Duration, stopChan chan struct{}) error {
	var (
		totalRequests      int
		successfulRequests int
		mux                sync.Mutex
		group              errgroup.Group
	)

	ctx.t.Logf("Maintaining %d concurrent requests for %v.", concurrency, duration)
	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			done := time.After(duration)
			client, err := pkgTest.NewSpoofingClient(ctx.clients.KubeClient, ctx.t.Logf, ctx.domain, test.ServingFlags.ResolvableDomain)
			if err != nil {
				return fmt.Errorf("error creating spoofing client: %v", err)
			}
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", ctx.domain), nil)
			if err != nil {
				return fmt.Errorf("error creating spoofing client: %v", err)
			}
			for {
				select {
				case <-stopChan:
					ctx.t.Log("Stopping generateTraffic")
					return nil
				case <-done:
					ctx.t.Log("Time up, done")
					return nil
				default:
					mux.Lock()
					requestID := totalRequests + 1
					totalRequests = requestID
					mux.Unlock()
					start := time.Now()
					res, err := client.Do(req)
					if err != nil {
						ctx.t.Logf("error making request %v", err)
						continue
					}
					duration := time.Now().Sub(start)
					ctx.t.Logf("Request took: %v", duration)

					if res.StatusCode != http.StatusOK {
						ctx.t.Logf("request %d failed with status %v", requestID, res.StatusCode)
						ctx.t.Logf("response headers: %v", res.Header)
						ctx.t.Logf("response body: %v", string(res.Body))
						continue
					}
					mux.Lock()
					successfulRequests++
					mux.Unlock()
				}
			}
		})
	}

	ctx.t.Log("Waiting for all requests to complete.")
	if err := group.Wait(); err != nil {
		return fmt.Errorf("error making requests for scale up: %v.", err)
	}

	if successfulRequests != totalRequests {
		return fmt.Errorf("error making requests for scale up. Got %d successful requests. Wanted %d.",
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
	names, err := CreateRouteAndConfig(t, clients, "autoscale", &test.Options{
		ContainerConcurrency: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	t.Log("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	err = test.WaitForRouteState(
		clients.ServingClient,
		names.Route,
		test.IsRouteReady,
		"RouteIsReady")
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	t.Log("Serves the expected data at the endpoint")
	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}
	names.Revision = config.Status.LatestCreatedRevisionName
	deploymentName := names.Revision + "-deployment"
	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		// Istio doesn't expose a status for us here: https://github.com/istio/istio/issues/6082
		// TODO(tcnghia): Remove this when https://github.com/istio/istio/issues/882 is fixed.
		pkgTest.Retrying(pkgTest.EventuallyMatchesBody(autoscaleExpectedOutput), http.StatusNotFound),
		"CheckingEndpointAfterUpdating",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%v\": %v",
			names.Route, domain, autoscaleExpectedOutput, err)
	}

	return &testContext{
		t:              t,
		clients:        clients,
		names:          names,
		deploymentName: deploymentName,
		domain:         domain,
	}
}

func assertScaleUp(ctx *testContext) {
	ctx.t.Log("The autoscaler spins up additional replicas when traffic increases.")
	err := generateTraffic(ctx, 20, 20*time.Second, nil)
	if err != nil {
		ctx.t.Fatalf("Error during initial scale up: %v", err)
	}
	ctx.t.Log("Waiting for scale up")
	err = pkgTest.WaitForDeploymentState(
		ctx.clients.KubeClient,
		ctx.deploymentName,
		isDeploymentScaledUp(),
		"DeploymentIsScaledUp",
		test.ServingNamespace,
		2*time.Minute)
	if err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling up. %s", ctx.deploymentName, err)
	}
}

func assertScaleDown(ctx *testContext) {
	ctx.t.Log("The autoscaler successfully scales down when devoid of traffic. Waiting for scale to zero.")
	err := pkgTest.WaitForDeploymentState(
		ctx.clients.KubeClient,
		ctx.deploymentName,
		test.DeploymentScaledToZeroFunc,
		"DeploymentScaledToZero",
		test.ServingNamespace,
		autoscaler.DefaultScaleToZeroGracePeriod+autoscaler.DefaultStableWindow+2*time.Minute)
	if err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down. %s", ctx.deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	ctx.t.Log("Wait for all pods to terminate.")

	err = pkgTest.WaitForPodListState(
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
		"WaitForAvailablePods", test.ServingNamespace)
	if err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods of %q: %v", ctx.deploymentName, err)
	}

	ctx.t.Log("Sleeping 10 seconds.")
	time.Sleep(10 * time.Second)
	ctx.t.Log("The Revision should remain ready after scaling to zero.")
	if err := test.CheckRevisionState(ctx.clients.ServingClient, ctx.names.Revision, test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to zero: %v", ctx.names.Revision, err)
	}

	ctx.t.Log("Scaled down.")
}

func TestAutoscaleUpDownUp(t *testing.T) {
	t.Parallel()
	ctx := setup(t)
	stopChan := DiagnoseMeEvery(t, 15*time.Second, ctx.clients)
	defer close(stopChan)
	defer test.TearDown(ctx.clients, ctx.names)

	assertScaleUp(ctx)
	assertScaleDown(ctx)
	assertScaleUp(ctx)
}

func assertNumberOfPods(ctx *testContext, numReplicasMin int32, numReplicasMax int32) error {
	deployment, err := ctx.clients.KubeClient.Kube.Apps().Deployments("serving-tests").Get(ctx.deploymentName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "Failed to get deployment %q", deployment)
	}
	gotReplicas := deployment.Status.Replicas
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

	// Allow some error to accumulate without locking
	errChan := make(chan error, 100)
	stopChan := make(chan struct{})
	defer close(stopChan)

	go func() {
		if err := generateTraffic(ctx, int(numPods*10), 30*time.Second, stopChan); err != nil {
			errChan <- err
		}
	}()

	done := make(chan struct{})
	timer := time.Tick(2 * time.Second)
	go func() {
		for {
			select {
			case <-stopChan:
				return
			case <-timer:
				if err := assertNumberOfPods(ctx, minPods, maxPods); err != nil {
					errChan <- err
				}
				if err := assertNumberOfPods(ctx, numPods, maxPods); err == nil {
					close(done)
				}
			}
		}
	}()

	select {
	case err := <-errChan:
		ctx.t.Error(err.Error())
	case <-done:
		// Success!
	}
}

func TestAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()
	ctx := setup(t)
	defer test.TearDown(ctx.clients, ctx.names)

	ctx.t.Log("The autoscaler spins up additional replicas when traffic increases.")
	// note: without the warm-up / gradual increase of load the test is retrieving a 503 (overload) from the envoy

	// Increase workload for 2 replicas for 30s
	// Assert the number of expected replicas is between n-1 and n+1, where n is the # of desired replicas for 30s.
	// Assert the number of expected replicas is n and n+1 at the end of 30s, where n is the # of desired replicas.
	assertAutoscaleUpToNumPods(ctx, 2)
	// Increase workload scale to 3 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	assertAutoscaleUpToNumPods(ctx, 3)
	// Increase workload scale to 4 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	assertAutoscaleUpToNumPods(ctx, 4)

}
