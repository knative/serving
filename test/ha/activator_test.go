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
)

// The Activator is managed by the activator HPA and does not have leader election enabled.
// The test ensures that stopping one of the activator pods doesn't affect user applications.
// One service is probed during activator restarts and another service is used for testing
// that we can scale from zero after activator restart.
func TestActivatorHA(t *testing.T) {
	clients := e2e.Setup(t)

	pods, err := clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).List(metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatalf("Failed to get activator pods: %v", err)
	}
	// Make sure we have the right number of activator replicas before continuing.
	if len(pods.Items) != haReplicas {
		t.Skipf("The test requires %d activator pods, got: %d", haReplicas, len(pods.Items))
	}
	// Sort the pods according to creation timestamp so that we can always kill the oldest one.
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].CreationTimestamp.Before(&pods.Items[j].CreationTimestamp) })

	// Create first service that we will continually probe during activator restart.
	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey:  "1",  //make sure we don't scale to zero during the test
			autoscaling.TargetBurstCapacityKey: "-1", // make sure all requests go through the activator
		}),
	)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	// Create second service that will be scaled to zero and after stopping the activator we'll
	// ensure it can be scaled back from zero.
	namesScaleToZero, resourcesScaleToZero := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(), //make sure we scale to zero quickly
			autoscaling.TargetBurstCapacityKey: "-1",                           // make sure all requests go through the activator
		}),
	)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, namesScaleToZero) })
	defer test.TearDown(clients, namesScaleToZero)

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resourcesScaleToZero.Revision), clients); err != nil {
		t.Fatalf("Failed to scale to zero: %v", err)
	}

	scaleToZeroURL := resources.Service.Status.URL.URL()
	prober := test.RunRouteProber(log.Printf, clients, resources.Service.Status.URL.URL())
	defer test.AssertProberDefault(t, prober)

	spoofingClient, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, scaleToZeroURL.Hostname(), test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	activatorPod := pods.Items[0].Name // stop the oldest activator pod

	clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).Delete(activatorPod, &metav1.DeleteOptions{
		GracePeriodSeconds: ptr.Int64(0),
	})

	// Wait for the killed activator to disappear from the knative service's endpoints.
	if err := waitForPublicEndpointAddresses(t, clients, resourcesScaleToZero.Revision.Name,
		1 /* expected number of public endpoint addresses */); err != nil {
		t.Fatal("Failed to wait for the service to use only the remaining activator")
	}

	// assert the service at the first possible moment - when the killed activator disappears from its endpoint address list
	assertServiceWorksNow(t, clients, spoofingClient, namesScaleToZero, scaleToZeroURL, test.PizzaPlanetText1)

	if err := waitForPodDeleted(t, clients, activatorPod); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", activatorPod, err)
	}
	if err := waitForDeploymentScale(clients, activatorDeploymentName, haReplicas); err != nil {
		t.Fatalf("Deployment %s failed to scale up: %v", activatorDeploymentName, err)
	}

	pods, err = clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).List(metav1.ListOptions{
		LabelSelector: activatorLabel,
	})
	if err != nil {
		t.Fatalf("Failed to get activator pods: %v", err)
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].CreationTimestamp.Before(&pods.Items[j].CreationTimestamp) })

	activatorPod = pods.Items[0].Name // stop the oldest activator pod again which is now a different one

	clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).Delete(activatorPod, &metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
	})

	if err := waitForPublicEndpointAddresses(t, clients, resourcesScaleToZero.Revision.Name,
		1 /* expected number of public endpoint addresses */); err != nil {
		t.Fatalf("Failed to wait for the service to be using only the remaining activator")
	}

	assertServiceWorksNow(t, clients, spoofingClient, namesScaleToZero, scaleToZeroURL, test.PizzaPlanetText1)

	if err := waitForPodDeleted(t, clients, activatorPod); err != nil {
		t.Fatalf("Did not observe %s to actually be deleted: %v", activatorPod, err)
	}
	if err := waitForDeploymentScale(clients, activatorDeploymentName, haReplicas); err != nil {
		t.Fatalf("Deployment %s failed to scale up: %v", activatorDeploymentName, err)
	}

	assertServiceEventuallyWorks(t, clients, namesScaleToZero, scaleToZeroURL, test.PizzaPlanetText1)
}
