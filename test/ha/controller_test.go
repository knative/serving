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
	"knative.dev/pkg/system"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	controllerDeploymentName = "controller"
)

func TestControllerHA(t *testing.T) {
	clients := e2e.Setup(t)

	if err := waitForDeploymentScale(clients, controllerDeploymentName, haReplicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", controllerDeploymentName, haReplicas, err)
	}

	leaderController, err := getLeader(t, clients, controllerDeploymentName)
	if err != nil {
		t.Fatal("Failed to get leader:", err)
	}

	service1Names, resources := createPizzaPlanetService(t)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, service1Names) })
	defer test.TearDown(clients, service1Names)

	clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).Delete(leaderController, &metav1.DeleteOptions{})

	if err := waitForPodDeleted(t, clients, leaderController); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", leaderController, err)
	}

	// Make sure a new leader has been elected
	if _, err = getLeader(t, clients, controllerDeploymentName); err != nil {
		t.Fatal("Failed to find new leader:", err)
	}

	assertServiceEventuallyWorks(t, clients, service1Names, resources.Service.Status.URL.URL(), test.PizzaPlanetText1)

	// Verify that after changing the leader we can still create a new kservice
	service2Names, _ := createPizzaPlanetService(t)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, service2Names) })
	test.TearDown(clients, service2Names)
}
