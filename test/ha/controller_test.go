// +build e2e

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

package ha

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	controllerDeploymentName = "controller"
)

func TestControllerHA(t *testing.T) {
	clients := e2e.Setup(t)

	if err := scaleUpDeployment(clients, controllerDeploymentName); err != nil {
		t.Fatalf("Failed to scale deployment: %v", err)
	}
	defer scaleDownDeployment(clients, controllerDeploymentName)
	test.CleanupOnInterrupt(func() { scaleDownDeployment(clients, controllerDeploymentName) })

	service1Names, resources := createPizzaPlanetService(t, "pizzaplanet-service1")
	test.CleanupOnInterrupt(func() { test.TearDown(clients, service1Names) })
	defer test.TearDown(clients, service1Names)

	leaderController, err := getLeader(t, clients, controllerDeploymentName)
	if err != nil {
		t.Fatalf("Failed to get leader: %v", err)
	}

	clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).Delete(leaderController, &metav1.DeleteOptions{})

	if err := waitForPodDeleted(t, clients, leaderController); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", leaderController, err)
	}

	// Make sure a new leader has been elected
	if _, err = getLeader(t, clients, controllerDeploymentName); err != nil {
		t.Fatalf("Failed to find new leader: %v", err)
	}

	assertServiceWorks(t, clients, service1Names, resources.Service.Status.URL.URL(), test.PizzaPlanetText1)

	// Verify that after changing the leader we can still create a new kservice
	service2Names, _ := createPizzaPlanetService(t, "pizzaplanet-service2")
	test.CleanupOnInterrupt(func() { test.TearDown(clients, service2Names) })
	test.TearDown(clients, service2Names)
}
