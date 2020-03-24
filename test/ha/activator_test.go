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
	"log"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	activatorDeploymentName = "activator"
	activatorLabel          = "app=activator"
)

// Activator is managed by the activator HPA and does not have leader election enabled.
// The test ensures that stopping one of the activator pods doesn't affect user applications.
func TestActivatorHA(t *testing.T) {
	clients := e2e.Setup(t)

	autoscalerConfigMap, err := e2e.RawCM(clients, autoscalerconfig.ConfigName)
	if err != nil {
		t.Errorf("Error retrieving autoscaler configmap: %v", err)
	}
	patchedAutoscalerConfigMap := autoscalerConfigMap.DeepCopy()
	// make sure all requests go through the activator
	patchedAutoscalerConfigMap.Data["target-burst-capacity"] = "-1"
	e2e.PatchCM(clients, patchedAutoscalerConfigMap)
	defer e2e.PatchCM(clients, autoscalerConfigMap)
	test.CleanupOnInterrupt(func() { e2e.PatchCM(clients, autoscalerConfigMap) })

	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey: "1", //make sure we don't scale to zero during the test
		}),
	)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	namesScaleToZero, resourcesScaleToZero := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey: autoscaling.WindowMin.String(), //make sure we scale to zero quickly
		}),
	)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, namesScaleToZero) })
	defer test.TearDown(clients, namesScaleToZero)

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resourcesScaleToZero.Revision), clients); err != nil {
		t.Fatalf("Failed to scale to zero: %v", err)
	}

	prober := test.RunRouteProber(log.Printf, clients, resources.Service.Status.URL.URL())
	defer test.AssertProberDefault(t, prober)

	pods, err := clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).List(metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatalf("Failed to get activator pods: %v", err)
	}
	if len(pods.Items) != haReplicas {
		t.Fatalf("The test requires %d activator pods", haReplicas)
	}
	activatorPod := pods.Items[0].Name // stop the first activator pod

	clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).Delete(activatorPod, &metav1.DeleteOptions{})

	if err := waitForPodDeleted(t, clients, activatorPod); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", activatorPod, err)
	}

	if err := waitForDeploymentScale(clients, activatorDeploymentName, haReplicas); err != nil {
		t.Fatalf("Deployment %s failed to scale up: %v", activatorDeploymentName, err)
	}

	activatorPod = pods.Items[1].Name // now stop the other activator pod

	clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).Delete(activatorPod, &metav1.DeleteOptions{})
	if err := waitForPodDeleted(t, clients, activatorPod); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", activatorPod, err)
	}

	if err := waitForDeploymentScale(clients, activatorDeploymentName, haReplicas); err != nil {
		t.Fatalf("Deployment %s failed to scale up: %v", activatorDeploymentName, err)
	}

	assertServiceWorks(t, clients, namesScaleToZero, resourcesScaleToZero.Service.Status.URL.URL(), test.PizzaPlanetText1)
}
