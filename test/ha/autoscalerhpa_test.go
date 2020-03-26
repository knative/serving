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
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	autoscalerHPALease          = "hpaautoscaler"
	autoscalerHPADeploymentName = "autoscaler-hpa"
)

func TestAutoscalerHPAHANewRevision(t *testing.T) {
	clients := e2e.Setup(t)

	if err := scaleUpDeployment(clients, autoscalerHPADeploymentName); err != nil {
		t.Fatalf("Failed to scale deployment: %v", err)
	}
	defer scaleDownDeployment(clients, autoscalerHPADeploymentName)
	test.CleanupOnInterrupt(func() { scaleDownDeployment(clients, autoscalerHPADeploymentName) })

	names, resources := createPizzaPlanetService(t, "pizzaplanet-service",
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.ClassAnnotationKey:  autoscaling.HPA,
			autoscaling.MetricAnnotationKey: autoscaling.CPU,
			autoscaling.TargetAnnotationKey: "70",
		}))
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	leaderController, err := getLeader(t, clients, autoscalerHPALease)
	if err != nil {
		t.Fatalf("Failed to get leader: %v", err)
	}

	clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).Delete(leaderController, &metav1.DeleteOptions{})

	if err := waitForPodDeleted(t, clients, leaderController); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", leaderController, err)
	}

	// Make sure a new leader has been elected
	if _, err = getLeader(t, clients, autoscalerHPALease); err != nil {
		t.Fatalf("Failed to find new leader: %v", err)
	}

	url := resources.Service.Status.URL.URL()
	assertServiceWorks(t, clients, names, url, test.PizzaPlanetText1)

	t.Log("Updating the Service after selecting new leader controller in order to generate a new revision")
	names.Image = test.PizzaPlanet2
	newImage := pkgTest.ImagePath(names.Image)
	if _, err := v1test.PatchService(t, clients, resources.Service, rtesting.WithServiceImage(newImage)); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, newImage, err)
	}

	t.Log("Service should be able to generate a new revision after changing the leader controller")
	names.Revision, err = v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("New image not reflected in Service: %v", err)
	}

	assertServiceWorks(t, clients, names, url, test.PizzaPlanetText2)
}
