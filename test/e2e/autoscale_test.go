// +build e2e

/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"math"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	autoscaleExpectedOutput = "39999983"
)

var (
	initialScaleToZeroThreshold string
)

func isExpectedOutput() func(body string) (bool, error) {
	return func(body string) (bool, error) {
		containsAutoscaleOutput := strings.Contains(body, autoscaleExpectedOutput)
		containsHelloworldOutput := strings.Contains(body, helloWorldExpectedOutput)
		return containsAutoscaleOutput || containsHelloworldOutput, nil
	}
}

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

func generateTrafficBurst(clients *test.Clients, names test.ResourceNames, num int, domain string) {
	concurrentRequests := make(chan bool, num)

	glog.Infof("Performing %d concurrent requests.", num)
	for i := 0; i < num; i++ {
		go func() {
			test.WaitForEndpointState(clients.Kube,
				test.Flags.ResolvableDomain,
				domain,
				NamespaceName,
				names.Route,
				isExpectedOutput())
			concurrentRequests <- true
		}()
	}

	glog.Infof("Waiting for all requests to complete.")
	for i := 0; i < num; i++ {
		<-concurrentRequests
	}
}

func getAutoscalerConfigMap(clients *test.Clients) (*v1.ConfigMap, error) {
	return clients.Kube.CoreV1().ConfigMaps("knative-serving-system").Get("config-autoscaler", metav1.GetOptions{})
}

func setScaleToZeroThreshold(clients *test.Clients, threshold string) error {
	configMap, err := getAutoscalerConfigMap(clients)
	if err != nil {
		return err
	}
	configMap.Data["scale-to-zero-threshold"] = threshold
	_, err = clients.Kube.CoreV1().ConfigMaps("knative-serving-system").Update(configMap)
	return err
}

func generateSustainedQpsTraffic(clients *test.Clients, names test.ResourceNames, qps int, duration time.Duration, domain string) float64 {
	glog.Infof("Generating %d QPS for %v", qps, duration)

	qpms := time.Duration(math.Ceil(float64(1000/qps))) * time.Millisecond
	tick := time.Tick(qpms)
	done := time.After(duration)
	totalRequests := 0
	successfulRequests := make(chan bool)
	for {
		select {
		case <-done:
			glog.Infof("Done generating %d total requests.", totalRequests)
			glog.Infof("Waiting for all requests to complete.")
			numSuccess := 0
			for i := 0; i < totalRequests; i++ {
				if <-successfulRequests {
					numSuccess++
				}

			}
			return (float64(numSuccess) / float64(totalRequests))
		case <-tick:
			totalRequests++

			go func() {
				err := test.WaitForEndpointState(clients.Kube,
					test.Flags.ResolvableDomain,
					domain,
					NamespaceName,
					names.Route,
					isExpectedOutput())
				successfulRequests <- (err == nil)
			}()
		}
	}
}

func setup(t *testing.T) *test.Clients {
	clients := Setup(t)

	configMap, err := getAutoscalerConfigMap(clients)
	if err != nil {
		glog.Infof("Unable to retrieve the autoscale configMap. Assuming a ScaleToZero value of '5m'. %v", err)
		initialScaleToZeroThreshold = "5m"
	} else {
		initialScaleToZeroThreshold = configMap.Data["scale-to-zero-threshold"]
	}
	return clients
}

func tearDown(clients *test.Clients, names test.ResourceNames) {
	setScaleToZeroThreshold(clients, initialScaleToZeroThreshold)
	TearDown(clients, names)
}

func TestAutoscaleUpDownUp(t *testing.T) {
	clients := setup(t)

	imagePath := strings.Join(
		[]string{
			test.Flags.DockerRepo,
			"autoscale"},
		"/")

	glog.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { tearDown(clients, names) })
	defer tearDown(clients, names)

	glog.Infof(`When the Revision can have traffic routed to it,
	            the Route is marked as Ready.`)
	err = test.WaitForRouteState(
		clients.Routes,
		names.Route,
		func(r *v1alpha1.Route) (bool, error) {
			return r.Status.IsReady(), nil
		})
	if err != nil {
		t.Fatalf(`The Route %s was not marked as Ready to serve traffic:
			 %v`, names.Route, err)
	}

	glog.Infof("Serves the expected data at the endpoint")
	config, err := clients.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(`Configuration %s was not updated with the new
		         revision: %v`, names.Config, err)
	}
	deploymentName :=
		config.Status.LatestCreatedRevisionName + "-deployment"
	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	err = test.WaitForEndpointState(
		clients.Kube,
		test.Flags.ResolvableDomain,
		domain,
		NamespaceName,
		names.Route,
		isExpectedOutput())
	if err != nil {
		t.Fatalf(`The endpoint for Route %s at domain %s didn't serve
			 the expected text \"%v\": %v`,
			names.Route, domain, autoscaleExpectedOutput, err)
	}

	glog.Infof(`The autoscaler spins up additional replicas when traffic
		    increases.`)
	generateTrafficBurst(clients, names, 5, domain)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledUp())
	if err != nil {
		glog.Fatalf(`Unable to observe the Deployment named %s scaling
			   up. %s`, deploymentName, err)
	}

	glog.Infof(`The autoscaler successfully scales down when devoid of
		    traffic.`)

	glog.Infof(`Manually setting ScaleToZeroThreshold to '1m' to facilitate
		    faster testing.`)

	err = setScaleToZeroThreshold(clients, "1m")
	if err != nil {
		t.Fatalf(`Unable to set ScaleToZeroThreshold to '1m'. This will
		          cause the test to time out. Failing fast instead. %v`, err)
	}

	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledToZero())
	if err != nil {
		glog.Fatalf(`Unable to observe the Deployment named %s scaling
		           down. %s`, deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	glog.Infof("Wait until there are pods available to scale into.")
	pc := clients.Kube.CoreV1().Pods(NamespaceName)

	err = test.WaitForPodListState(
		pc,
		func(p *v1.PodList) (bool, error) {
			return len(p.Items) == 0, nil
		})

	glog.Infof("Scaled down, resetting ScaleToZeroThreshold.")
	setScaleToZeroThreshold(clients, initialScaleToZeroThreshold)
	glog.Infof(`The autoscaler spins up additional replicas once again when
              traffic increases.`)
	generateTrafficBurst(clients, names, 8, domain)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledUp())
	if err != nil {
		glog.Fatalf(`Unable to observe the Deployment named %s scaling
			   up. %s`, deploymentName, err)
	}
}

func TestAutoscaleErrorRatesToFreshlyReservedRevision(t *testing.T) {
	clients := setup(t)

	imagePath := strings.Join(
		[]string{
			test.Flags.DockerRepo,
			"helloworld"},
		"/")

	glog.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { tearDown(clients, names) })
	defer tearDown(clients, names)

	glog.Infof(`When the Revision can have traffic routed to it,
	            the Route is marked as Ready.`)
	err = test.WaitForRouteState(
		clients.Routes,
		names.Route,
		func(r *v1alpha1.Route) (bool, error) {
			return r.Status.IsReady(), nil
		})
	if err != nil {
		t.Fatalf(`The Route %s was not marked as Ready to serve traffic:
			 %v`, names.Route, err)
	}

	glog.Infof("Serves the expected data at the endpoint")
	config, err := clients.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(`Configuration %s was not updated with the new
		         revision: %v`, names.Config, err)
	}
	deploymentName :=
		config.Status.LatestCreatedRevisionName + "-deployment"
	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	err = test.WaitForEndpointState(
		clients.Kube,
		test.Flags.ResolvableDomain,
		domain,
		NamespaceName,
		names.Route,
		isExpectedOutput())
	if err != nil {
		t.Fatalf(`The endpoint for Route %s at domain %s didn't serve
			 the expected text \"%v\": %v`,
			names.Route, domain, helloWorldExpectedOutput, err)
	}

	glog.Infof("Put the Revision in Reserved state by scaling to 0.")
	glog.Infof(`Manually setting ScaleToZeroThreshold to '1m' to facilitate
		    faster testing.`)

	err = setScaleToZeroThreshold(clients, "1m")
	if err != nil {
		t.Fatalf(`Unable to set ScaleToZeroThreshold to '1m'. This will
		          cause the test to time out. Failing fast instead. %v`, err)
	}

	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledToZero())
	if err != nil {
		glog.Fatalf(`Unable to observe the Deployment named %s scaling
		           down. %s`, deploymentName, err)
	}

	glog.Infof("Scaled down, resetting ScaleToZeroThreshold.")
	setScaleToZeroThreshold(clients, initialScaleToZeroThreshold)
	glog.Infof(`The autoscaler spins up additional replicas once again when
              traffic increases.`)

	successRate := generateSustainedQpsTraffic(clients, names, 10, 1*time.Minute, domain)
	minSuccessRate := 0.99
	if successRate < minSuccessRate {
		t.Fatalf("Expected a success rate greater than %0.3f, but got %0.3f", minSuccessRate, successRate)
	}
	glog.Infof("Completed the sustained QPS test with a success rate of %0.3f", successRate)
}

func TestAutoscaleErrorRatesToFullyReservedRevision(t *testing.T) {
	clients := setup(t)

	imagePath := strings.Join(
		[]string{
			test.Flags.DockerRepo,
			"helloworld"},
		"/")

	glog.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { tearDown(clients, names) })
	defer tearDown(clients, names)

	glog.Infof(`When the Revision can have traffic routed to it,
	            the Route is marked as Ready.`)
	err = test.WaitForRouteState(
		clients.Routes,
		names.Route,
		func(r *v1alpha1.Route) (bool, error) {
			return r.Status.IsReady(), nil
		})
	if err != nil {
		t.Fatalf(`The Route %s was not marked as Ready to serve traffic:
			 %v`, names.Route, err)
	}

	glog.Infof("Serves the expected data at the endpoint")
	config, err := clients.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(`Configuration %s was not updated with the new
		         revision: %v`, names.Config, err)
	}
	deploymentName :=
		config.Status.LatestCreatedRevisionName + "-deployment"
	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	err = test.WaitForEndpointState(
		clients.Kube,
		test.Flags.ResolvableDomain,
		domain,
		NamespaceName,
		names.Route,
		isExpectedOutput())
	if err != nil {
		t.Fatalf(`The endpoint for Route %s at domain %s didn't serve
			 the expected text \"%v\": %v`,
			names.Route, domain, helloWorldExpectedOutput, err)
	}

	glog.Infof("Put the Revision in Reserved state by scaling to 0.")
	glog.Infof(`Manually setting ScaleToZeroThreshold to '1m' to facilitate
		    faster testing.`)

	err = setScaleToZeroThreshold(clients, "1m")
	if err != nil {
		t.Fatalf(`Unable to set ScaleToZeroThreshold to '1m'. This will
		          cause the test to time out. Failing fast instead. %v`, err)
	}

	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledToZero())
	if err != nil {
		glog.Fatalf(`Unable to observe the Deployment named %s scaling
		           down. %s`, deploymentName, err)
	}

	glog.Infof("Wait until all pods are fully terminated.")
	pc := clients.Kube.CoreV1().Pods(NamespaceName)

	err = test.WaitForPodListState(
		pc,
		func(p *v1.PodList) (bool, error) {
			return len(p.Items) == 0, nil
		})

	glog.Infof("Scaled down, resetting ScaleToZeroThreshold.")
	glog.Infof("Scaled down, resetting ScaleToZeroThreshold.")
	setScaleToZeroThreshold(clients, initialScaleToZeroThreshold)
	glog.Infof(`The autoscaler spins up additional replicas once again when
              traffic increases.`)

	successRate := generateSustainedQpsTraffic(clients, names, 10, 1*time.Minute, domain)
	minSuccessRate := 0.99
	if successRate < minSuccessRate {
		t.Fatalf("Expected a success rate greater than %0.3f, but got %0.3f", minSuccessRate, successRate)
	}
	glog.Infof("Completed the sustained QPS test with a success rate of %0.3f", successRate)
}
