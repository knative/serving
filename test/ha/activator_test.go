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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
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
	SLO                     = 0.99 // We permit 0.01 of requests to fail due to killing the Activator.
)

// The Activator does not have leader election enabled.
// The test ensures that stopping one of the activator pods doesn't affect user applications.
// One service is probed during activator restarts and another service is used for testing
// that we can scale from zero after activator restart.
func TestActivatorHA(t *testing.T) {
	clients := e2e.Setup(t)

	if err := waitForDeploymentScale(clients, activatorDeploymentName, haReplicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", activatorDeploymentName, haReplicas, err)
	}

	// Create first service that we will continually probe during activator restart.
	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey:  "1",  // Make sure we don't scale to zero during the test.
			autoscaling.TargetBurstCapacityKey: "-1", // Make sure all requests go through the activator.
		}),
	)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	// Create second service that will be scaled to zero and after stopping the activator we'll
	// ensure it can be scaled back from zero.
	namesScaleToZero, resourcesScaleToZero := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(), // Make sure we scale to zero quickly.
			autoscaling.TargetBurstCapacityKey: "-1",                           // Make sure all requests go through the activator.
		}),
	)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, namesScaleToZero) })
	defer test.TearDown(clients, namesScaleToZero)

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resourcesScaleToZero.Revision), clients); err != nil {
		t.Fatal("Failed to scale to zero:", err)
	}

	prober := test.RunRouteProber(log.Printf, clients, resources.Service.Status.URL.URL())
	defer assertSLO(t, prober)

	scaleToZeroURL := resourcesScaleToZero.Service.Status.URL.URL()
	spoofingClient, err := pkgTest.NewSpoofingClient(
		clients.KubeClient,
		t.Logf,
		scaleToZeroURL.Hostname(),
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))

	if err != nil {
		t.Fatal("Error creating spoofing client:", err)
	}

	pods, err := clients.KubeClient.Kube.CoreV1().Pods(test.ServingFlags.SystemNamespace).List(metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatal("Failed to get activator pods:", err)
	}
	activatorPod := pods.Items[0].Name

	origEndpoints, err := getPublicEndpoints(t, clients, resourcesScaleToZero.Revision.Name)
	if err != nil {
		t.Fatalf("Unable to get public endpoints for revision %s: %v", resourcesScaleToZero.Revision.Name, err)
	}

	clients.KubeClient.Kube.CoreV1().Pods(test.ServingFlags.SystemNamespace).Delete(activatorPod, &metav1.DeleteOptions{
		GracePeriodSeconds: ptr.Int64(0),
	})

	// Wait for the killed activator to disappear from the knative service's endpoints.
	if err := waitForChangedPublicEndpoints(t, clients, resourcesScaleToZero.Revision.Name, origEndpoints); err != nil {
		t.Fatal("Failed to wait for the service to update its endpoints:", err)
	}

	// Assert the service at the first possible moment after the killed activator disappears from its endpoints.
	assertServiceWorksNow(t, clients, spoofingClient, namesScaleToZero, scaleToZeroURL, test.PizzaPlanetText1)

	if err := waitForPodDeleted(t, clients, activatorPod); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", activatorPod, err)
	}
	if err := waitForDeploymentScale(clients, activatorDeploymentName, haReplicas); err != nil {
		t.Fatalf("Deployment %s failed to scale up: %v", activatorDeploymentName, err)
	}

	pods, err = clients.KubeClient.Kube.CoreV1().Pods(test.ServingFlags.SystemNamespace).List(metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatal("Failed to get activator pods:", err)
	}

	// Sort the pods according to creation timestamp so that we can kill the oldest one. We want to
	// gradually kill both activator pods that were started at the beginning.
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].CreationTimestamp.Before(&pods.Items[j].CreationTimestamp) })

	activatorPod = pods.Items[0].Name // Stop the oldest activator pod remaining.

	origEndpoints, err = getPublicEndpoints(t, clients, resourcesScaleToZero.Revision.Name)
	if err != nil {
		t.Fatalf("Unable to get public endpoints for revision %s: %v", resourcesScaleToZero.Revision.Name, err)
	}

	clients.KubeClient.Kube.CoreV1().Pods(test.ServingFlags.SystemNamespace).Delete(activatorPod, &metav1.DeleteOptions{
		GracePeriodSeconds: ptr.Int64(0),
	})

	// Wait for the killed activator to disappear from the knative service's endpoints.
	if err := waitForChangedPublicEndpoints(t, clients, resourcesScaleToZero.Revision.Name, origEndpoints); err != nil {
		t.Fatal("Failed to wait for the service to update its endpoints:", err)
	}

	// Assert the service at the first possible moment after the killed activator disappears from its endpoints.
	assertServiceWorksNow(t, clients, spoofingClient, namesScaleToZero, scaleToZeroURL, test.PizzaPlanetText1)
}

func assertSLO(t *testing.T, p test.Prober) {
	t.Helper()
	if err := p.Stop(); err != nil {
		t.Error("Failed to stop prober:", err)
	}
	if err := test.CheckSLO(SLO, t.Name(), p); err != nil {
		t.Error("CheckSLO failed:", err)
	}
}
