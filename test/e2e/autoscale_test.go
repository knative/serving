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

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/test"
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

func generateTrafficBurst(clients *test.Clients, num int, domain string) {
	concurrentRequests := make(chan bool, num)

	log.Printf("Performing %d concurrent requests.", num)
	for i := 0; i < num; i++ {
		go func() {
			test.WaitForEndpointState(clients.Kube,
				test.Flags.ResolvableDomain,
				domain,
				NamespaceName,
				RouteName,
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
	defer TearDown(clients)

	imagePath := strings.Join(
		[]string{
			test.Flags.DockerRepo,
			NamespaceName + "-autoscale"},
		"/")

	log.Println("Creating a new Route and Configuration")
	err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}

	log.Println(`When the Revision can have traffic routed to it,
	            the Route is marked as Ready.`)
	err = test.WaitForRouteState(
		clients.Routes,
		RouteName,
		func(r *v1alpha1.Route) (bool, error) {
			return r.Status.IsReady(), nil
		})
	if err != nil {
		t.Fatalf(`The Route %s was not marked as Ready to serve traffic:
			 %v`, RouteName, err)
	}

	log.Println("Serves the expected data at the endpoint")
	config, err := clients.Configs.Get(ConfigName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(`Configuration %s was not updated with the new
		         revision: %v`, ConfigName, err)
	}
	deploymentName :=
		config.Status.LatestCreatedRevisionName + "-deployment"
	route, err := clients.Routes.Get(RouteName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", RouteName, err)
	}
	domain := route.Status.Domain

	err = test.WaitForEndpointState(
		clients.Kube,
		test.Flags.ResolvableDomain,
		domain,
		NamespaceName,
		RouteName,
		isExpectedOutput())
	if err != nil {
		t.Fatalf(`The endpoint for Route %s at domain %s didn't serve
			 the expected text \"%v\": %v`,
			RouteName, domain, autoscaleExpectedOutput, err)
	}

	log.Println(`The autoscaler spins up additional replicas when traffic
		    increases.`)
	generateTrafficBurst(clients, 5, domain)
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
	generateTrafficBurst(clients, 8, domain)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledUp())
	if err != nil {
		log.Fatalf(`Unable to observe the Deployment named %s scaling
			   up. %s`, deploymentName, err)
	}
}
