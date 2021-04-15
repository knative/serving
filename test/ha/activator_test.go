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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	pkgnet "knative.dev/pkg/network"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/networking"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	activatorDeploymentName = "activator"
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
	ctx := context.Background()

	podDeleteOptions := metav1.DeleteOptions{GracePeriodSeconds: gracePeriod}
	var err error
	var desiredScale int

	if desiredScale, err = waitForActivatorScale(ctx, clients.KubeClient); err != nil {
		t.Fatalf("Deployment %s not scaled > 1: %s", activatorDeploymentName, err)
	}

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

	t.Log("Starting prober")
	prober := test.NewProberManager(t.Logf, clients, minProbes)
	prober.Spawn(resources.Service.Status.URL.URL())
	defer assertSLO(t, prober, slo)

	activatorPods, err := gatherBackingActivators(ctx, clients.KubeClient, test.ServingNamespace, resources.Revision.Name, resourcesScaleToZero.Revision.Name)
	if err != nil {
		t.Fatal("failed to gather backing activators:", err)
	}

	t.Logf("Rolling %d activators", len(activatorPods))
	for i, activatorPod := range activatorPods {
		t.Logf("Waiting for %s to scale to zero", namesScaleToZero.Revision)
		if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resourcesScaleToZero.Revision), clients); err != nil {
			t.Fatal("Failed to scale to zero:", err)
		}

		t.Logf("Deleting activator%d (%s)", i, activatorPod.name)
		if err := clients.KubeClient.CoreV1().Pods(system.Namespace()).Delete(ctx, activatorPod.name, podDeleteOptions); err != nil {
			t.Fatalf("Failed to delete pod %s: %v", activatorPod.name, err)
		}

		// Wait for the killed activator to disappear from the knative service's endpoints.
		if err := waitForEndpointsState(clients.KubeClient, resourcesScaleToZero.Revision.Name, test.ServingNamespace, readyEndpointsDoNotContain(activatorPod.ip)); err != nil {
			t.Fatal("Failed to wait for the service to update its endpoints:", err)
		}
		if gracePeriod != nil && *gracePeriod == 0 {
			t.Log("Allow the network to notice the missing endpoint")
			time.Sleep(pkgnet.DefaultDrainTimeout)
		}

		t.Log("Test if service still works")
		assertServiceEventuallyWorks(t, clients, namesScaleToZero, resourcesScaleToZero.Service.Status.URL.URL(), test.PizzaPlanetText1, test.ServingFlags.ResolvableDomain)

		t.Logf("Wait for activator%d (%s) to vanish", i, activatorPod.name)
		if err := pkgTest.WaitForPodDeleted(ctx, clients.KubeClient, activatorPod.name, system.Namespace()); err != nil {
			t.Fatalf("Did not observe %s to actually be deleted: %v", activatorPod.name, err)
		}
		// Check for the endpoint to appear in the activator's endpoints, since this revision may pick a subset of those endpoints.
		t.Logf("Wait for a new activator to spin up")
		if err := pkgTest.WaitForServiceEndpoints(ctx, clients.KubeClient, networking.ActivatorServiceName, system.Namespace(), desiredScale); err != nil {
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

type activatorPod struct {
	name string
	ip   string
}

func gatherBackingActivators(ctx context.Context, client kubernetes.Interface, namespace string, revs ...string) ([]activatorPod, error) {
	podMap := make(map[string]activatorPod)

	for _, rev := range revs {
		endpoints := client.CoreV1().Endpoints(namespace)
		e, err := endpoints.Get(ctx, rev, metav1.GetOptions{})

		if err != nil {
			return nil, fmt.Errorf("failed to gather %s endpoints: %w", rev, err)
		}

		for _, subset := range e.Subsets {
			for _, address := range subset.Addresses {
				if address.TargetRef == nil {
					return nil, fmt.Errorf("%s service is not pointing to a pod", rev)
				}

				name := address.TargetRef.Name
				if !strings.HasPrefix(name, activatorDeploymentName) {
					return nil, fmt.Errorf("%s service is not pointing to an activator pod but: %s", rev, address.TargetRef.Name)
				}

				podMap[name] = activatorPod{name: name, ip: address.IP}
			}
		}
	}

	pods := make([]activatorPod, 0, len(podMap))
	for _, pod := range podMap {
		pods = append(pods, pod)
	}

	return pods, nil
}

func waitForActivatorScale(ctx context.Context, client kubernetes.Interface) (int, error) {
	desiredScale := 0
	check := func(d *appsv1.Deployment) (bool, error) {
		if *d.Spec.Replicas < 2 {
			return false, errors.New("spec.replicaCount should be > 1")
		}
		desiredScale = int(*d.Spec.Replicas)
		return d.Status.ReadyReplicas > 1, nil
	}

	err := pkgTest.WaitForDeploymentState(
		ctx,
		client,
		activatorDeploymentName,
		check,
		"ActivatorIsScaled",
		system.Namespace(),
		time.Minute,
	)

	return desiredScale, err
}
