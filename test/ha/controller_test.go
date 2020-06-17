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
	pkgTest "knative.dev/pkg/test"
	pkgHa "knative.dev/pkg/test/ha"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const controllerDeploymentName = "controller"

func TestControllerHA(t *testing.T) {
	clients := e2e.Setup(t)
	cancel := logstream.Start(t)
	defer cancel()

	if err := pkgTest.WaitForDeploymentScale(clients.KubeClient, controllerDeploymentName, system.Namespace(), haReplicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", controllerDeploymentName, haReplicas, err)
	}

	leaderController, err := pkgHa.WaitForNewLeader(clients.KubeClient, controllerDeploymentName, system.Namespace(), "" /*use arbitrary name as there was no previous leader*/)
	if err != nil {
		t.Fatal("Failed to get leader:", err)
	}

	service1Names, resources := createPizzaPlanetService(t)
	test.EnsureTearDown(t, clients, service1Names)

	if err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).Delete(leaderController,
		&metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete pod %s: %v", leaderController, err)
	}

	if err := pkgTest.WaitForPodDeleted(clients.KubeClient, leaderController, system.Namespace()); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", leaderController, err)
	}

	// Make sure a new leader has been elected
	if _, err = pkgHa.WaitForNewLeader(clients.KubeClient, controllerDeploymentName, system.Namespace(), leaderController); err != nil {
		t.Fatal("Failed to find new leader:", err)
	}

	assertServiceEventuallyWorks(t, clients, service1Names, resources.Service.Status.URL.URL(), test.PizzaPlanetText1)

	// Verify that after changing the leader we can still create a new kservice
	service2Names, _ := createPizzaPlanetService(t)
	test.EnsureTearDown(t, clients, service2Names)
}
