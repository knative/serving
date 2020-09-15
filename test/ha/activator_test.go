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
	"log"
	"testing"
	"time"

	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgnet "knative.dev/pkg/network"
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
	testActivatorHA(t, nil, 1)
}

func TestActivatorHANonGraceful(t *testing.T) {
	// For non-graceful tests, we want the pod to receive a SIGKILL straight away.
	testActivatorHA(t, ptr.Int64(0), 0.90)
}

// The Activator does not have leader election enabled.
// The test ensures that stopping one of the activator pods doesn't affect user applications.
// One service is probed during activator restarts and another service is used for testing
// that we can scale from zero after activator restart.
func testActivatorHA(t *testing.T, gracePeriod *int64, slo float64) {
	clients := e2e.Setup(t)

	podDeleteOptions := metav1.DeleteOptions{GracePeriodSeconds: gracePeriod}

	if err := pkgTest.WaitForDeploymentScale(context.Background(), clients.KubeClient, activatorDeploymentName, system.Namespace(), test.ServingFlags.Replicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", activatorDeploymentName, test.ServingFlags.Replicas, err)
	}
	activators, err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).List(context.Background(), metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatal("Failed to get activator pods:", err)
	}

	// Create first service that we will continually probe during activator restart.
	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey:  "1",  // Make sure we don't scale to zero during the test.
			autoscaling.TargetBurstCapacityKey: "-1", // Make sure all requests go through the activator.
		}),
	)
	test.EnsureTearDown(t, clients, &names)

	// Create second service that will be scaled to zero and after stopping the activator we'll
	// ensure it can be scaled back from zero.
	namesScaleToZero, resourcesScaleToZero := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(), // Make sure we scale to zero quickly.
			autoscaling.TargetBurstCapacityKey: "-1",                           // Make sure all requests go through the activator.
		}),
	)
	test.EnsureTearDown(t, clients, &namesScaleToZero)

	t.Logf("Waiting for %s to scale to zero", namesScaleToZero.Revision)
	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resourcesScaleToZero.Revision), clients); err != nil {
		t.Fatal("Failed to scale to zero:", err)
	}

	t.Log("Starting prober")
	prober := test.NewProberManager(log.Printf, clients, minProbes)
	prober.Spawn(resources.Service.Status.URL.URL())
	defer assertSLO(t, prober, slo)

	for i, activator := range activators.Items {
		t.Logf("Deleting activator%d (%s)", i, activator.Name)
		if err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).Delete(context.Background(), activator.Name, podDeleteOptions); err != nil {
			t.Fatalf("Failed to delete pod %s: %v", activator.Name, err)
		}

		// Wait for the killed activator to disappear from the knative service's endpoints.
		if err := waitForEndpointsState(clients.KubeClient, resourcesScaleToZero.Revision.Name, test.ServingNamespace, readyEndpointsDoNotContain(activator.Status.PodIP)); err != nil {
			t.Fatal("Failed to wait for the service to update its endpoints:", err)
		}
		if gracePeriod != nil && *gracePeriod == 0 {
			t.Log("Allow the network to notice the missing endpoint")
			time.Sleep(pkgnet.DefaultDrainTimeout)
		}

		t.Log("Test if service still works")
		assertServiceEventuallyWorks(t, clients, namesScaleToZero, resourcesScaleToZero.Service.Status.URL.URL(), test.PizzaPlanetText1)

		t.Logf("Wait for activator%d (%s) to vanish", i, activator.Name)
		if err := pkgTest.WaitForPodDeleted(context.Background(), clients.KubeClient, activator.Name, system.Namespace()); err != nil {
			t.Fatalf("Did not observe %s to actually be deleted: %v", activator.Name, err)
		}
		// Check for the endpoint to appear in the activator's endpoints, since this revision may pick a subset of those endpoints.
		if err := pkgTest.WaitForServiceEndpoints(context.Background(), clients.KubeClient, networking.ActivatorServiceName, system.Namespace(), test.ServingFlags.Replicas); err != nil {
			t.Fatalf("Deployment %s failed to scale up: %v", activatorDeploymentName, err)
		}
		if gracePeriod != nil && *gracePeriod == 0 {
			t.Log("Allow the network to notice the new endpoint")
			time.Sleep(pkgnet.DefaultDrainTimeout)
		}
	}
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
