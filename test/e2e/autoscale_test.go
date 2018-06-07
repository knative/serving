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
	"log"
	"strings"
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	autoscaleExpectedOutput = "39999983"
)

func isExpectedOutput() func(body string) (bool, error) {
	return func(body string) (bool, error) {
		return strings.Contains(body, autoscaleExpectedOutput), nil
	}
}

func isDeploymentScaledUp() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas > 1, nil
	}
}

func isDeploymentScaledDown() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas <= 1, nil
	}
}

func generateTrafficBurst(clients *test.Clients, names test.ResourceNames, num int, domain string) {
	concurrentRequests := make(chan bool, num)

	log.Printf("Performing %d concurrent requests.", num)
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

	log.Println("Waiting for all requests to complete.")
	for i := 0; i < num; i++ {
		<-concurrentRequests
	}
}

func TestAutoscaleUpDownUp(t *testing.T) {
	clients := Setup(t)

	imagePath := strings.Join(
		[]string{
			test.Flags.DockerRepo,
			"autoscale"},
		"/")

	log.Println("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, names) })
	defer TearDown(clients, names)

	log.Println(`When the Revision can have traffic routed to it,
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

	log.Println("Serves the expected data at the endpoint")
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

	log.Println(`The autoscaler spins up additional replicas when traffic
		    increases.`)
	generateTrafficBurst(clients, names, 5, domain)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledUp())
	if err != nil {
		log.Fatalf(`Unable to observe the Deployment named %s scaling
			   up. %s`, deploymentName, err)
	}

	log.Println(`The autoscaler successfully scales down when devoid of
		    traffic.`)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledDown())
	if err != nil {
		log.Fatalf(`Unable to observe the Deployment named %s scaling
		           down. %s`, deploymentName, err)
	}

	// Account for the case where scaling up uses all available pods.
	log.Println("Wait until there are pods available to scale into.")
	pc := clients.Kube.CoreV1().Pods(NamespaceName)
	pods, err := pc.List(metav1.ListOptions{})
	podCount := 0
	if err != nil {
		log.Printf("Unable to get pod count. Defaulting to 1.")
		podCount = 1
	} else {
		podCount = len(pods.Items)
	}
	err = test.WaitForPodListState(
		pc,
		func(p *v1.PodList) (bool, error) {
			return len(p.Items) < podCount, nil
		})

	log.Println(`The autoscaler spins up additional replicas once again when
	            traffic increases.`)
	generateTrafficBurst(clients, names, 8, domain)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledUp())
	if err != nil {
		log.Fatalf(`Unable to observe the Deployment named %s scaling
			   up. %s`, deploymentName, err)
	}
}
