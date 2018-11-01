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

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	"golang.org/x/sync/errgroup"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	autoscaleExpectedOutput = "399989"
)

var (
	scaleToZeroThreshold time.Duration
)

func isDeploymentScaledUp() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas > 1, nil
	}
}

func isDeploymentScaledToZero() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas == 0, nil
	}
}

func tearDown(ctx *testContext) {
	TearDown(ctx.clients, ctx.names, ctx.logger)
}

func generateTraffic(ctx *testContext, concurrency int, duration time.Duration) error {
	var (
		totalRequests      int
		successfulRequests int
		mux                sync.Mutex
		group              errgroup.Group
	)

	ctx.logger.Infof("Maintaining %d concurrent requests for %v.", concurrency, duration)
	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			done := time.After(duration)
			client, err := pkgTest.NewSpoofingClient(ctx.clients.KubeClient, ctx.logger, ctx.domain, test.ServingFlags.ResolvableDomain)
			if err != nil {
				return fmt.Errorf("error creating spoofing client: %v", err)
			}
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", ctx.domain), nil)
			if err != nil {
				return fmt.Errorf("error creating spoofing client: %v", err)
			}
			for {
				select {
				case <-done:
					return nil
				default:
					mux.Lock()
					requestID := totalRequests + 1
					totalRequests = requestID
					mux.Unlock()
					start := time.Now()
					res, err := client.Do(req)
					if err != nil {
						ctx.logger.Infof("error making request %v", err)
						continue
					}
					duration := time.Now().Sub(start)
					ctx.logger.Infof("Request took: %v", duration)

					if res.StatusCode != http.StatusOK {
						ctx.logger.Infof("request %d failed", requestID)
						ctx.logger.Infof("non 200 response %v", res.StatusCode)
						ctx.logger.Infof("response headers: %v", res.Header)
						ctx.logger.Infof("response body: %v", string(res.Body))
						continue
					}
					mux.Lock()
					successfulRequests++
					mux.Unlock()
				}
			}
		})
	}

	ctx.logger.Infof("Waiting for all requests to complete.")
	if err := group.Wait(); err != nil {
		return fmt.Errorf("Error making requests for scale up: %v.", err)
	}

	if successfulRequests != totalRequests {
		return fmt.Errorf("Error making requests for scale up. Got %d successful requests. Wanted %d.",
			successfulRequests, totalRequests)
	}
	return nil
}

type testContext struct {
	t              *testing.T
	clients        *test.Clients
	logger         *logging.BaseLogger
	names          test.ResourceNames
	deploymentName string
	domain         string
}

func setup(t *testing.T) *testContext {
	//add test case specific name to its own logger
	logger := logging.GetContextLogger(t.Name())
	clients := Setup(t)

	configMap, err := test.GetConfigMap(clients.KubeClient).Get("config-autoscaler", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unable to get autoscaler config map: %v", err)
	}
	scaleToZeroThreshold, err = time.ParseDuration(configMap.Data["scale-to-zero-threshold"])
	if err != nil {
		t.Fatalf("Unable to parse scale-to-zero-threshold as duration: %v", err)
	}

	imagePath := test.ImagePath("autoscale")

	logger.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, logger, imagePath, &test.Options{
		ContainerConcurrency: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	err = test.WaitForRouteState(
		clients.ServingClient,
		names.Route,
		test.IsRouteReady,
		"RouteIsReady")
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	logger.Infof("Serves the expected data at the endpoint")
	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}
	deploymentName :=
		config.Status.LatestCreatedRevisionName + "-deployment"
	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		// Istio doesn't expose a status for us here: https://github.com/istio/istio/issues/6082
		// TODO(tcnghia): Remove this when https://github.com/istio/istio/issues/882 is fixed.
		pkgTest.Retrying(pkgTest.EventuallyMatchesBody(autoscaleExpectedOutput), http.StatusNotFound, http.StatusServiceUnavailable),
		"CheckingEndpointAfterUpdating",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%v\": %v",
			names.Route, domain, autoscaleExpectedOutput, err)
	}

	return &testContext{
		t:              t,
		clients:        clients,
		logger:         logger,
		names:          names,
		deploymentName: deploymentName,
		domain:         domain,
	}
}

func assertScaleUp(ctx *testContext) {
	ctx.logger.Infof("The autoscaler spins up additional replicas when traffic increases.")
	err := generateTraffic(ctx, 20, 20*time.Second)
	if err != nil {
		ctx.t.Fatalf("Error during initial scale up: %v", err)
	}
	ctx.logger.Info("Waiting for scale up")
	err = test.WaitForDeploymentState(
		ctx.clients.KubeClient,
		ctx.deploymentName,
		isDeploymentScaledUp(),
		"DeploymentIsScaledUp",
		2*time.Minute)
	if err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling up. %s", ctx.deploymentName, err)
	}
}

func assertScaleDown(ctx *testContext) {
	ctx.logger.Infof("The autoscaler successfully scales down when devoid of traffic.")

	ctx.logger.Infof("Waiting for scale to zero")
	err := test.WaitForDeploymentState(
		ctx.clients.KubeClient,
		ctx.deploymentName,
		isDeploymentScaledToZero(),
		"DeploymentScaledToZero",
		scaleToZeroThreshold+2*time.Minute)
	if err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down. %s", ctx.deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	ctx.logger.Infof("Wait for all pods to terminate.")

	err = test.WaitForPodListState(
		ctx.clients.KubeClient,
		func(p *v1.PodList) (bool, error) {
			for _, pod := range p.Items {
				if !strings.Contains(pod.Status.Reason, "Evicted") {
					return false, nil
				}
			}
			return true, nil
		},
		"WaitForAvailablePods")
	if err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods: %v", err)
	}

	ctx.logger.Infof("Scaled down.")
}

func TestAutoscaleUpDownUp(t *testing.T) {
	ctx := setup(t)
	stopChan := DiagnoseMeEvery(15*time.Second, ctx.clients, ctx.logger)
	defer close(stopChan)
	defer tearDown(ctx)

	assertScaleUp(ctx)
	assertScaleDown(ctx)
	assertScaleUp(ctx)
}