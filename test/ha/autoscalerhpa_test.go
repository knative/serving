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
	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const autoscalerHPADeploymentName = "autoscaler-hpa"

func TestAutoscalerHPAHANewRevision(t *testing.T) {
	clients := e2e.Setup(t)

	if err := pkgTest.WaitForDeploymentScale(context.Background(), clients.KubeClient, autoscalerHPADeploymentName, system.Namespace(), test.ServingFlags.Replicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", autoscalerHPADeploymentName, test.ServingFlags.Replicas, err)
	}

	// TODO(mattmoor): Once we switch to the new sharded leader election, we should use more than a single bucket here, but the test is still interesting.
	leaders, err := pkgHa.WaitForNewLeaders(context.Background(), t, clients.KubeClient, autoscalerHPADeploymentName, system.Namespace(), sets.NewString(), test.ServingFlags.Buckets)
	if err != nil {
		t.Fatal("Failed to get leader:", err)
	}
	t.Log("Got initial leader set:", leaders)

	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.ClassAnnotationKey:  autoscaling.HPA,
			autoscaling.MetricAnnotationKey: autoscaling.CPU,
			autoscaling.TargetAnnotationKey: "70",
		}))

	test.EnsureTearDown(t, clients, &names)

	for _, leader := range leaders.List() {
		if err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).Delete(context.Background(), leader,
			metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
			t.Fatalf("Failed to delete pod %s: %v", leader, err)
		}

		if err := pkgTest.WaitForPodDeleted(context.Background(), clients.KubeClient, leader, system.Namespace()); err != nil {
			t.Fatalf("Did not observe %s to actually be deleted: %v", leader, err)
		}
	}

	// Wait for all of the old leaders to go away, and then for the right number to be back.
	if _, err := pkgHa.WaitForNewLeaders(context.Background(), t, clients.KubeClient, autoscalerHPADeploymentName, system.Namespace(), leaders, test.ServingFlags.Buckets); err != nil {
		t.Fatal("Failed to find new leader:", err)
	}

	url := resources.Service.Status.URL.URL()
	assertServiceEventuallyWorks(t, clients, names, url, test.PizzaPlanetText1)

	t.Log("Updating the Service after selecting new leader controller in order to generate a new revision")
	names.Image = test.PizzaPlanet2
	newImage := pkgTest.ImagePath(names.Image)
	if _, err := v1test.PatchService(t, clients, resources.Service, rtesting.WithServiceImage(newImage)); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, newImage, err)
	}

	t.Log("Service should be able to generate a new revision after changing the leader controller")
	names.Revision, err = v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatal("New image not reflected in Service:", err)
	}

	assertServiceEventuallyWorks(t, clients, names, url, test.PizzaPlanetText2)
}
