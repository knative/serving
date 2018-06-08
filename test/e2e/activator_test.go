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
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func isDeploymentScaledTo0() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas == 0, nil
	}
}

func isDeploymentScaledTo1() func(d *v1beta1.Deployment) (bool, error) {
	return func(d *v1beta1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas == 1, nil
	}
}

func TestScaleBetween0And1(t *testing.T) {
	clients := Setup(t)

	configMap, err := clients.Kube.CoreV1().ConfigMaps("knative-serving-system").Get("config-autoscaler", metav1.GetOptions{})
	configMap.Data["enable-scale-to-zero"] = "true"
	configMap.Data["scale-to-zero-threshold"] = "1m"
	_, err = clients.Kube.CoreV1().ConfigMaps("knative-serving-system").Update(configMap)

	var imagePath string
	imagePath = strings.Join([]string{test.Flags.DockerRepo, "helloworld"}, "/")

	log.Println("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}

	test.CleanupOnInterrupt(func() {
		TearDown(clients, names)
	})

	defer TearDown(clients, names)

	log.Printf("Waiting for Route %s is marked as Ready... ", names.Route)
	err = test.WaitForRouteState(clients.Routes, names.Route, func(r *v1alpha1.Route) (bool, error) {
		return r.Status.IsReady(), nil
	})
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}
	log.Println("Done.")

	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	config, err := clients.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(`Configuration %s was not updated with the new
		         revision: %v`, names.Config, err)
	}

	deploymentName :=
		config.Status.LatestCreatedRevisionName + "-deployment"
	domain := route.Status.Domain
	log.Printf("Waiting for Route %s deployment %s endpoint with domain %s ready... ", names.Route, deploymentName, domain)
	err = test.WaitForEndpointState(clients.Kube, test.Flags.ResolvableDomain, domain, NamespaceName, names.Route, isHelloWorldExpectedOutput())
	if err != nil {
		t.Fatalf("The endpoint for Route %s deployment %s at domain %s didn't serve the expected text \"%s\": %v",
			names.Route, deploymentName, domain, helloWorldExpectedOutput, err)
	}
	log.Println("Done.")

	log.Printf("Verifying the deployment %s is scaled to 1... ", deploymentName)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledTo1())
	if err != nil {
		log.Fatalf(`Unable to observe the Deployment named %s scaled to 1. %v`, deploymentName, err)
	}
	log.Println("Done.")

	log.Println("Sleeping 65 seconds for no activity. The deployment should be scaled to 0 afterwards.")
	time.Sleep(65 * time.Second)
	log.Printf("Verifying the deployment %s is scaled to 0... ", deploymentName)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledTo0())
	if err != nil {
		log.Fatalf(`Unable to observe the Deployment named %s scaled to 0. %v`, deploymentName, err)
	}
	log.Println("Done.")

	log.Println("Sleeping 60 seconds for k8s resources to be teared down.")
	time.Sleep(60 * time.Second)
	log.Println("Sending a request should activate the revision")
	err = test.WaitForEndpointState(clients.Kube, test.Flags.ResolvableDomain, domain, NamespaceName, names.Route, isHelloWorldExpectedOutput())
	if err != nil {
		t.Fatalf("The endpoint for Route %s deployment %s at domain %s didn't serve the expected text \"%s\": %v",
			names.Route, deploymentName, domain, helloWorldExpectedOutput, err)
	}

	log.Printf("Verifying the deployment %s is scaled to 1 again... ", deploymentName)
	err = test.WaitForDeploymentState(
		clients.Kube.ExtensionsV1beta1().Deployments(NamespaceName),
		deploymentName,
		isDeploymentScaledTo1())
	if err != nil {
		log.Fatalf(`Unable to observe the Deployment named %s scaled to 1. %v`, deploymentName, err)
	}
	log.Println("Done.")
}
