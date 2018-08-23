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
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	autoscaleExpectedOutput = "39999983"
)

var (
	initialScaleToZeroThreshold   string
	initialScaleToZeroGracePeriod string
)

func isDeploymentScaledUp() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas >= 1, nil
	}
}

func isDeploymentScaledToZero() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas == 0, nil
	}
}

func generateTrafficBurst(clients *test.Clients, logger *logging.BaseLogger, want int, domain string) error {
	concurrentRequests := make(chan bool, want)

	logger.Infof("Performing %d concurrent requests.", want)
	for i := 0; i < want; i++ {
		go func() {
			res, err := pkgTest.WaitForEndpointState(clients.KubeClient,
				logger,
				domain,
				pkgTest.Retrying(pkgTest.EventuallyMatchesBody(autoscaleExpectedOutput), http.StatusNotFound),
				"MakingConcurrentRequests",
				test.ServingFlags.ResolvableDomain)
			if err != nil {
				logger.Errorf("Unsuccessful request: %v", err)
				if res != nil {
					logger.Errorf("Response headers: %v", res.Header)
				} else {
					logger.Errorf("Nil response. No response headers to show.")
				}
			}
			concurrentRequests <- err == nil
		}()
	}

	logger.Infof("Waiting for all requests to complete.")
	got := 0
	for i := 0; i < want; i++ {
		if <-concurrentRequests {
			got++
		}
	}
	if got != want {
		return fmt.Errorf("Error making requests for scale up. Got %v successful requests. Wanted %v.", got, want)
	}
	return nil
}

func getAutoscalerConfigMap(clients *test.Clients) (*v1.ConfigMap, error) {
	return test.GetConfigMap(clients.KubeClient).Get("config-autoscaler", metav1.GetOptions{})
}

func setScaleToZeroThreshold(clients *test.Clients, threshold string, gracePeriod string) error {
	configMap, err := getAutoscalerConfigMap(clients)
	if err != nil {
		return err
	}
	configMap.Data["scale-to-zero-threshold"] = threshold
	configMap.Data["scale-to-zero-grace-period"] = gracePeriod
	_, err = test.GetConfigMap(clients.KubeClient).Update(configMap)
	return err
}

func setup(t *testing.T, logger *logging.BaseLogger) *test.Clients {
	clients := Setup(t)

	configMap, err := getAutoscalerConfigMap(clients)
	if err != nil {
		logger.Infof("Unable to retrieve the autoscale configMap. Assuming a ScaleToZero value of '5m'. %v", err)
		initialScaleToZeroThreshold = "5m"
		initialScaleToZeroGracePeriod = "2m"
	} else {
		initialScaleToZeroThreshold = configMap.Data["scale-to-zero-threshold"]
		initialScaleToZeroGracePeriod = configMap.Data["scale-to-zero-grace-period"]
	}

	err = setScaleToZeroThreshold(clients, "1m", "30s")
	if err != nil {
		t.Fatalf(`Unable to set ScaleToZeroThreshold to '1m'. This will
		          cause the test to time out. Failing fast instead. %v`, err)
	}

	return clients
}

func tearDown(clients *test.Clients, names test.ResourceNames, logger *logging.BaseLogger) {
	setScaleToZeroThreshold(clients, initialScaleToZeroThreshold, initialScaleToZeroGracePeriod)
	TearDown(clients, names, logger)
}

func TestAutoscaleUpDownUp(t *testing.T) {
	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestAutoscaleUpDownUp")

	clients := setup(t, logger)
	imagePath := strings.Join(
		[]string{
			pkgTest.Flags.DockerRepo,
			"autoscale"},
		"/")

	logger.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, logger, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { tearDown(clients, names, logger) }, logger)
	defer tearDown(clients, names, logger)

	logger.Infof(`When the Revision can have traffic routed to it,
	            the Route is marked as Ready.`)
	err = test.WaitForRouteState(
		clients.ServingClient,
		names.Route,
		test.IsRouteReady,
		"RouteIsReady")
	if err != nil {
		t.Fatalf(`The Route %s was not marked as Ready to serve traffic:
			 %v`, names.Route, err)
	}

	logger.Infof("Serves the expected data at the endpoint")
	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(`Configuration %s was not updated with the new
		         revision: %v`, names.Config, err)
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
		t.Fatalf(`The endpoint for Route %s at domain %s didn't serve
			 the expected text \"%v\": %v`,
			names.Route, domain, autoscaleExpectedOutput, err)
	}

	logger.Infof(`The autoscaler spins up additional replicas when traffic
		    increases.`)
	err = generateTrafficBurst(clients, logger, 5, domain)
	if err != nil {
		logger.Fatalf("Error during initial scale up: %v", err)
	}
	err = test.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		isDeploymentScaledUp(),
		"DeploymentIsScaledUp")
	if err != nil {
		logger.Fatalf(`Unable to observe the Deployment named %s scaling
			   up. %s`, deploymentName, err)
	}

	logger.Infof(`The autoscaler successfully scales down when devoid of
		    traffic.`)

	logger.Infof(`Manually setting ScaleToZeroThreshold to '1m' to facilitate
		    faster testing.`)

	err = test.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		isDeploymentScaledToZero(),
		"DeploymentScaledToZero")
	if err != nil {
		logger.Fatalf(`Unable to observe the Deployment named %s scaling
		           down. %s`, deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	logger.Infof("Wait for all pods to terminate.")

	err = test.WaitForPodListState(
		clients.KubeClient,
		func(p *v1.PodList) (bool, error) {
			return len(p.Items) == 0, nil
		},
		"WaitForAvailablePods")
	if err != nil {
		logger.Fatalf(`Waiting for Pod.List to have no items: %v`, err)
	}

	logger.Infof("Scaled down.")
	logger.Infof(`The autoscaler spins up additional replicas once again when
              traffic increases.`)
	err = generateTrafficBurst(clients, logger, 8, domain)
	if err != nil {
		t.Fatalf("Error during final scale up: %v", err)
	}
	err = test.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		isDeploymentScaledUp(),
		"DeploymentScaledUp")
	if err != nil {
		logger.Fatalf(`Unable to observe the Deployment named %s scaling
			   up. %s`, deploymentName, err)
	}
}
