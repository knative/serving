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
	"net/http"
	"strconv"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	helloWorldExpectedOutput = "Hello World! How about some tasty noodles?"
)

func TestHelloWorld(t *testing.T) {
	clients := Setup(t)

	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestHelloWorld")

	var imagePath = test.ImagePath("helloworld")

	logger.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, logger, imagePath, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(pkgTest.MatchesBody(helloWorldExpectedOutput), http.StatusNotFound),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}

	var revName string
	err = test.WaitForConfigurationState(clients.ServingClient, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
			revName = c.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ConfigurationUpdatedWithRevision")

	if err != nil {
		t.Fatalf("Error fetching Revision %v", err)
	}

	revision, err := clients.ServingClient.Revisions.Get(revName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Revision %s: %v", revName, err)
	}

	if val, ok := revision.Labels["serving.knative.dev/configuration"]; ok {
		if val != names.Config {
			t.Fatalf("Expect confguration name in revision label %q but got %q ", names.Config, val)
		}
	} else {
		t.Fatalf("Failed to get configuration name from Revision label")
	}
	if val, ok := revision.Labels["serving.knative.dev/service"]; ok {
		if val != names.Service {
			t.Fatalf("Expect Service name in revision label %q but got %q ", names.Service, val)
		}
	} else {
		t.Fatalf("Failed to get Service name from Revision label")
	}
}

func TestHelloWorldUserPort(t *testing.T) {
	var userPort int32
	userPort = 8888
	clients := Setup(t)

	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestHelloWorld-UserPort")

	var imagePath = test.ImagePath("helloworld-userport")

	logger.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, logger, imagePath, &test.Options{
		ContainerPorts: generateUserPort(logger, userPort),
	})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(pkgTest.MatchesBody(helloWorldExpectedOutput), http.StatusNotFound),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}

	logger.Infof("Check user deployment's Ports and Env info.")
	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}
	deploymentName :=
		config.Status.LatestCreatedRevisionName + "-deployment"
	deploy, err := clients.KubeClient.Kube.ExtensionsV1beta1().Deployments("serving-tests").Get(deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment %v, %v", deploymentName, err)
	}

	userContainer, found := findUserContainer(deploy.Spec.Template.Spec.Containers, "user-container")
	if !found {
		t.Fatalf("Failed to find deployment %v's user container", deploymentName)
	}
	found = findExpectPortInContainer(userContainer, v1alpha1.RevisionContainerUserPortName, userPort)
	if !found {
		t.Fatalf("Failed to find deployment %v's user container's user-port", deploymentName)
	}
	found = findExpectEnvInContainer(userContainer, "PORT", strconv.Itoa(int(userPort)))
	if !found {
		t.Fatalf("Failed to find deployment %v's user container's PORT env", deploymentName)
	}

	queueProxyContainer, found := findUserContainer(deploy.Spec.Template.Spec.Containers, "queue-proxy")
	if !found {
		t.Fatalf("Failed to find deployment %v's queue-proxy", deploymentName)
	}
	found = findExpectEnvInContainer(queueProxyContainer, "USER_PORT", strconv.Itoa(int(userPort)))
	if !found {
		t.Fatalf("Failed to find deployment %v's queue-proxy's USER_PORT env", deploymentName)
	}
}

func findUserContainer(containers []v1.Container, containerName string) (*v1.Container, bool) {
	for _, container := range containers {
		if container.Name == containerName {
			return &container, true
		}
	}
	return nil, false
}

func findExpectPortInContainer(contaienr *v1.Container, expectPortName string, expectPortValue int32) bool {
	for _, port := range contaienr.Ports {
		if port.Name == expectPortName && port.ContainerPort == expectPortValue {
			return true
		}
	}
	return false
}

func findExpectEnvInContainer(contaienr *v1.Container, expectEnvName string, expectEnvValue string) bool {
	for _, env := range contaienr.Env {
		if env.Name == expectEnvName && env.Value == expectEnvValue {
			return true
		}
	}
	return false
}

func generateUserPort(logger *logging.BaseLogger, port int32) []v1.ContainerPort {
	containerPorts := []v1.ContainerPort{}
	userPort := v1.ContainerPort{
		Name:          "user-port",
		ContainerPort: int32(port),
	}
	logger.Infof("set user port: %v", port)

	containerPorts = append(containerPorts, userPort)
	return containerPorts
}
