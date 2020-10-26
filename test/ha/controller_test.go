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
	"context"
	"testing"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	pkgHa "knative.dev/pkg/test/ha"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	controllerDeploymentName = "controller"
)

func TestControllerHA(t *testing.T) {
	clients := e2e.Setup(t)

	if err := pkgTest.WaitForDeploymentScale(context.Background(), clients.KubeClient, controllerDeploymentName, system.Namespace(), test.ServingFlags.Replicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", controllerDeploymentName, test.ServingFlags.Replicas, err)
	}

	// TODO(mattmoor): Once we switch to the new sharded leader election, we should use more than a single bucket here, but the test is still interesting.
	leaders, err := pkgHa.WaitForNewLeaders(context.Background(), t, clients.KubeClient, controllerDeploymentName, system.Namespace(), sets.NewString(), NumControllerReconcilers*test.ServingFlags.Buckets)
	if err != nil {
		t.Fatal("Failed to get leader:", err)
	}
	t.Log("Got initial leader set:", leaders)

	service1Names, resources := createPizzaPlanetService(t)
	test.EnsureTearDown(t, clients, &service1Names)

	prober := test.RunRouteProber(t.Logf, clients, resources.Service.Status.URL.URL())
	defer test.AssertProberDefault(t, prober)

	for _, leader := range leaders.List() {
		if err := clients.KubeClient.CoreV1().Pods(system.Namespace()).Delete(context.Background(), leader,
			metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
			t.Fatalf("Failed to delete pod %s: %v", leader, err)
		}
		if err := pkgTest.WaitForPodDeleted(context.Background(), clients.KubeClient, leader, system.Namespace()); err != nil {
			t.Fatalf("Did not observe %s to actually be deleted: %v", leader, err)
		}
	}

	// Wait for all of the old leaders to go away, and then for the right number to be back.
	if _, err := pkgHa.WaitForNewLeaders(context.Background(), t, clients.KubeClient, controllerDeploymentName, system.Namespace(), leaders, NumControllerReconcilers*test.ServingFlags.Buckets); err != nil {
		t.Fatal("Failed to find new leader:", err)
	}

	// Verify that after changing the leader we can still create a new kservice
	service2Names, _ := createPizzaPlanetService(t)
	test.EnsureTearDown(t, clients, &service2Names)
}
