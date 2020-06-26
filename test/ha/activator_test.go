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
	"sort"
	"testing"

	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	"knative.dev/pkg/test/logstream"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	activatorDeploymentName = "activator"
	activatorLabel          = "app=activator"
	minProbes               = 400 // We want to send at least 400 requests.
)

func TestActivatorHAGraceful(t *testing.T) {
	t.Skip("TODO(8066): This was added too optimistically. Needs debugging and triaging.")
	testActivatorHA(t, nil, 1)
}

func TestActivatorHANonGraceful(t *testing.T) {
	// For non-graceful tests, we want the pod to receive a SIGKILL straight away.
	testActivatorHA(t, ptr.Int64(0), 0.95)
}

// The Activator does not have leader election enabled.
// The test ensures that stopping one of the activator pods doesn't affect user applications.
// One service is probed during activator restarts and another service is used for testing
// that we can scale from zero after activator restart.
func testActivatorHA(t *testing.T, gracePeriod *int64, slo float64) {
	clients := e2e.Setup(t)
	cancel := logstream.Start(t)
	defer cancel()

	podDeleteOptions := &metav1.DeleteOptions{GracePeriodSeconds: gracePeriod}

	if err := pkgTest.WaitForDeploymentScale(clients.KubeClient, activatorDeploymentName, system.Namespace(), haReplicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", activatorDeploymentName, haReplicas, err)
	}

	// Create first service that we will continually probe during activator restart.
	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey:  "1",  // Make sure we don't scale to zero during the test.
			autoscaling.TargetBurstCapacityKey: "-1", // Make sure all requests go through the activator.
		}),
	)
	test.EnsureTearDown(t, clients, names)

	// Create second service that will be scaled to zero and after stopping the activator we'll
	// ensure it can be scaled back from zero.
	namesScaleToZero, resourcesScaleToZero := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(), // Make sure we scale to zero quickly.
			autoscaling.TargetBurstCapacityKey: "-1",                           // Make sure all requests go through the activator.
		}),
	)
	test.EnsureTearDown(t, clients, namesScaleToZero)

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resourcesScaleToZero.Revision), clients); err != nil {
		t.Fatal("Failed to scale to zero:", err)
	}

	prober := test.NewProberManager(log.Printf, clients, minProbes)
	prober.Spawn(resources.Service.Status.URL.URL())
	defer assertSLO(t, prober, slo)

	pods, err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).List(metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatal("Failed to get activator pods:", err)
	}
	activatorPod := pods.Items[0].Name

	origEndpoints, err := pkgTest.GetEndpointAddresses(clients.KubeClient, resourcesScaleToZero.Revision.Name, test.ServingNamespace)
	if err != nil {
		t.Fatalf("Unable to get public endpoints for revision %s: %v", resourcesScaleToZero.Revision.Name, err)
	}

	if err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).Delete(activatorPod, podDeleteOptions); err != nil {
		t.Fatalf("Failed to delete pod %s: %v", activatorPod, err)
	}

	// Wait for the killed activator to disappear from the knative service's endpoints.
	if err := pkgTest.WaitForChangedEndpoints(clients.KubeClient, resourcesScaleToZero.Revision.Name, test.ServingNamespace, origEndpoints); err != nil {
		t.Fatal("Failed to wait for the service to update its endpoints:", err)
	}

	assertServiceEventuallyWorks(t, clients, namesScaleToZero, resourcesScaleToZero.Service.Status.URL.URL(), test.PizzaPlanetText1)

	if err := pkgTest.WaitForPodDeleted(clients.KubeClient, activatorPod, system.Namespace()); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", activatorPod, err)
	}
	if err := pkgTest.WaitForDeploymentScale(clients.KubeClient, activatorDeploymentName, system.Namespace(), haReplicas); err != nil {
		t.Fatalf("Deployment %s failed to scale up: %v", activatorDeploymentName, err)
	}

	pods, err = clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).List(metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatal("Failed to get activator pods:", err)
	}

	// Sort the pods according to creation timestamp so that we can kill the oldest one. We want to
	// gradually kill both activator pods that were started at the beginning.
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].CreationTimestamp.Before(&pods.Items[j].CreationTimestamp) })

	activatorPod = pods.Items[0].Name // Stop the oldest activator pod remaining.

	origEndpoints, err = pkgTest.GetEndpointAddresses(clients.KubeClient, resourcesScaleToZero.Revision.Name, test.ServingNamespace)
	if err != nil {
		t.Fatalf("Unable to get public endpoints for revision %s: %v", resourcesScaleToZero.Revision.Name, err)
	}

	if err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).Delete(activatorPod, podDeleteOptions); err != nil {
		t.Fatalf("Failed to delete pod %s: %v", activatorPod, err)
	}

	// Wait for the killed activator to disappear from the knative service's endpoints.
	if err := pkgTest.WaitForChangedEndpoints(clients.KubeClient, resourcesScaleToZero.Revision.Name, test.ServingNamespace, origEndpoints); err != nil {
		t.Fatal("Failed to wait for the service to update its endpoints:", err)
	}

	assertServiceEventuallyWorks(t, clients, namesScaleToZero, resourcesScaleToZero.Service.Status.URL.URL(), test.PizzaPlanetText1)
}

func assertSLO(t *testing.T, p test.Prober, slo float64) {
	t.Helper()
	if err := p.Stop(); err != nil {
		t.Error("Failed to stop prober:", err)
	}
	if err := test.CheckSLO(slo, t.Name(), p); err != nil {
		t.Error("CheckSLO failed:", err)
	}
}
