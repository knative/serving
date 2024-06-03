//go:build e2e
// +build e2e

/*
Copyright 2024 The Knative Authors

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
	"strconv"
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	minimumNumberOfReplicas = 2
	maximumNumberOfReplicas = 2
)

func TestActivatorNotInRequestPath(t *testing.T) {
	clients := e2e.Setup(t)
	ctx := context.Background()

	// Create first service that we will continually probe during disruption scenario.
	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey:  strconv.Itoa(minimumNumberOfReplicas), // Make sure we don't scale to zero during the test.
			autoscaling.MaxScaleAnnotationKey:  strconv.Itoa(maximumNumberOfReplicas),
			autoscaling.TargetBurstCapacityKey: "0", // The Activator is only added to the request path during scale from zero scenarios.
		}),
	)
	test.EnsureTearDown(t, clients, &names)

	testUptimeDuringUserPodDeletion(t, ctx, clients, names, resources)
}

func TestActivatorInRequestPathAlways(t *testing.T) {
	clients := e2e.Setup(t)
	ctx := context.Background()

	// Create first service that we will continually probe during disruption scenario.
	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey:  strconv.Itoa(minimumNumberOfReplicas), // Make sure we don't scale to zero during the test.
			autoscaling.MaxScaleAnnotationKey:  strconv.Itoa(maximumNumberOfReplicas),
			autoscaling.TargetBurstCapacityKey: "-1", // Make sure all requests go through the activator.
		}),
	)
	test.EnsureTearDown(t, clients, &names)

	testUptimeDuringUserPodDeletion(t, ctx, clients, names, resources)
}

func TestActivatorInRequestPathPossibly(t *testing.T) {
	clients := e2e.Setup(t)
	ctx := context.Background()

	// Create first service that we will continually probe during disruption scenario.
	names, resources := createPizzaPlanetService(t,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey:  strconv.Itoa(minimumNumberOfReplicas), // Make sure we don't scale to zero during the test.
			autoscaling.MaxScaleAnnotationKey:  strconv.Itoa(maximumNumberOfReplicas),
			autoscaling.TargetBurstCapacityKey: "1", // The Activator may be in the path, depending on the revision scale and load.
		}),
	)
	test.EnsureTearDown(t, clients, &names)

	testUptimeDuringUserPodDeletion(t, ctx, clients, names, resources)
}

func testUptimeDuringUserPodDeletion(t *testing.T, ctx context.Context, clients *test.Clients, names test.ResourceNames, resources *v1test.ResourceObjects) {
	t.Log("Starting prober")
	prober := test.NewProberManager(t.Logf, clients, minProbes, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))

	prober.Spawn(resources.Service.Status.URL.URL())
	defer assertSLO(t, prober, 1)

	// Get user pods

	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: names.Service,
	})
	pods, err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		t.Fatalf("Unable to get pods: %v", err)
	}

	if !(len(pods.Items) == minimumNumberOfReplicas) {
		t.Fatalf("Expected to have %d user pod(s) running, but found %d.", minimumNumberOfReplicas, len(pods.Items))
	}

	t.Logf("Watching user pods")
	var wg sync.WaitGroup
	wg.Add(2)

	go watchPodEvents(t, ctx, clients, &wg, selector, pods.Items[0].Name)
	go watchPodEvents(t, ctx, clients, &wg, selector, pods.Items[1].Name)
	wg.Wait()

	// Delete user pods
	for _, pod := range pods.Items {
		err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Unable to delete pod: %v", err)
		}
	}

	newPods, err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		t.Fatalf("Unable to get pods: %v", err)
	}

	if !(len(newPods.Items) == minimumNumberOfReplicas) {
		t.Errorf("Expected to have %d user pod(s) running, but found %d.", minimumNumberOfReplicas, len(pods.Items))
	}
}

func watchPodEvents(t *testing.T, ctx context.Context, clients *test.Clients, wg *sync.WaitGroup, selector labels.Selector, targetPod string) {
	defer wg.Done()

	watcher, err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
		Watch:         true,
	})
	if err != nil {
		t.Errorf("Unable to watch pods: %v", err)
		panic(err)
	}

	podEventsChan := watcher.ResultChan()
	defer watcher.Stop()

	for event := range podEventsChan {
		pod, ok := event.Object.(*v1.Pod)
		t.Logf("Pod %s received event: %s", pod.Name, event.Type)
		if !ok {
			continue
		}
		if event.Type == watch.Deleted && pod.Name == targetPod {
			t.Logf("Pod %s deleted from node %s", pod.Name, pod.Spec.NodeName)
			break
		}
	}
}
