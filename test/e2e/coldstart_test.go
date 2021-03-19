// +build e2e

/*
Copyright 2021 The Knative Authors

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

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"
)

func TestColdStartFocused(t *testing.T) {
	// Not running in parallel on purpose.

	ctx := context.Background()
	clients := Setup(t)

	image := "helloworld"

	// Launch a DaemonSet to make sure the image is prepulled to all nodes.
	preloadImage(t, clients, image)

	filter := map[string]string{
		"knative.dev/test-cold-start": "true",
	}
	watcher, err := clients.KubeClient.CoreV1().Pods(test.ServingNamespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(filter).String(),
	})
	if err != nil {
		t.Fatal("Failed to setup watch for pods", err)
	}
	t.Cleanup(watcher.Stop)

	// The amount of samples to generate.
	n := 10

	createdCh := make(chan time.Time, n)
	readyCh := make(chan time.Time, n)
	terminatingCh := make(chan string, n)
	deletedCh := make(chan struct{}, n)
	go func() {
		terminating := make(map[string]bool, n)
		ready := make(map[string]bool, n)
		for event := range watcher.ResultChan() {
			now := time.Now()
			switch event.Type {
			case watch.Added:
				createdCh <- now
			case watch.Modified:
				p := event.Object.(*corev1.Pod)
				if isPodConditionTrue(p, corev1.PodReady) && ready[p.Name] == false {
					ready[p.Name] = true
					readyCh <- now
				}
				if p.DeletionTimestamp != nil && terminating[p.Name] == false {
					terminating[p.Name] = true
					terminatingCh <- p.Name
				}
			case watch.Deleted:
				deletedCh <- struct{}{}
			}
		}
	}()

	var total time.Duration
	readinessLatencies := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		names := test.ResourceNames{
			Config: test.ObjectNameForTest(t),
			Image:  image,
		}
		configuration, err := v1test.CreateConfiguration(t, clients, names, func(c *v1.Configuration) {
			c.Spec.Template.Labels = filter
			c.Spec.Template.Annotations = map[string]string{
				autoscaling.MinScaleAnnotationKey: "1", // Make sure the pod doesn't scale down prematurely.
			}
		})
		if err != nil {
			t.Fatal("Failed to create configuration", err)
		}
		t.Cleanup(func() {
			clients.ServingClient.Configs.Delete(ctx, names.Config, metav1.DeleteOptions{})
		})

		if err := v1test.WaitForConfigurationState(clients.ServingClient, names.Config, v1test.IsConfigurationReady, "ConfigurationIsReady"); err != nil {
			t.Fatalf("Configuration %s did not become ready: %v", names.Config, err)
		}

		// Read "exact" creation and ready time for the current pod.
		createdTime := <-createdCh
		readyTime := <-readyCh
		latency := readyTime.Sub(createdTime)
		total += latency
		readinessLatencies = append(readinessLatencies, latency)

		if err := clients.ServingClient.Configs.Delete(ctx, configuration.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal("Failed to remove configuration", err)
		}

		// Speed up pod removal by forcing it to shut down now.
		// Wait for the pod to become terminating.
		podName := <-terminatingCh
		if err := clients.KubeClient.CoreV1().Pods(test.ServingNamespace).Delete(ctx, podName,
			metav1.DeleteOptions{
				GracePeriodSeconds: ptr.Int64(0),
			}); err != nil {
			t.Fatal("Failed to remove pods quickly", err)
		}

		// Wait for the pod to actually vanish.
		<-deletedCh
	}

	avg := total / time.Duration(n)
	max := 6 * time.Second
	if avg > max {
		t.Errorf("Average cold-start time was %v, want less than %v", avg, max)
		t.Errorf("Data: %v", readinessLatencies)
	}
}

func preloadImage(t *testing.T, clients *test.Clients, image string) {
	ctx := context.Background()
	fullImageName := pkgTest.ImagePath(image)

	for prefix := range shared.DigestResolutionExceptions {
		if strings.HasPrefix(fullImageName, prefix) {
			// Skip preloading for all of the local versions.
			// TODO(markusthoemmes): Find a better solution.
			return
		}
	}

	// Launch a DaemonSet to make sure the image is prepulled to all nodes.
	dsName := test.ObjectNameForTest(t)
	dsLabels := map[string]string{
		"knative.dev/test-cold-start-warmup": dsName,
	}
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: test.ServingNamespace,
			Name:      test.ObjectNameForTest(t),
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: dsLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: dsLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "warmup",
						Image: pkgTest.ImagePath(image),
					}},
				},
			},
		},
	}

	if _, err := clients.KubeClient.AppsV1().DaemonSets(ds.Namespace).Create(ctx, ds, metav1.CreateOptions{}); err != nil {
		t.Fatal("Failed to create DaemonSet to preload images", err)
	}
	t.Cleanup(func() {
		clients.KubeClient.AppsV1().DaemonSets(ds.Namespace).Delete(ctx, ds.Name, metav1.DeleteOptions{})
	})

	if err := wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		got, err := clients.KubeClient.AppsV1().DaemonSets(ds.Namespace).Get(ctx, ds.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return got.Status.NumberReady > 0 && got.Status.NumberReady == got.Status.DesiredNumberScheduled, nil
	}); err != nil {
		t.Fatal("DaemonSet never successfully deployed", err)
	}
}

func isPodConditionTrue(p *corev1.Pod, condType corev1.PodConditionType) bool {
	for _, cond := range p.Status.Conditions {
		if cond.Type == condType && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
