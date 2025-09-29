//go:build e2e
// +build e2e

/*
Copyright 2019 The Knative Authors

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
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestMinScaleTransition(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		minScale      = 4
		scalingWindow = autoscaling.WindowMin
	)

	clients := Setup(t)
	cmClient := clients.KubeClient.CoreV1().ConfigMaps(test.ServingFlags.TestNamespace)

	names := test.ResourceNames{
		// Config and Route have different names to avoid false positives
		Config: test.ObjectNameForTest(t),
		Route:  test.ObjectNameForTest(t),
		Image:  test.ConfigMimic,
	}

	test.EnsureTearDown(t, clients, &names)

	secondRevisionName := kmeta.ChildName(names.Config, fmt.Sprintf("-%05d", 2))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Config,
			Namespace: test.ServingFlags.TestNamespace,
		},
		Data: map[string]string{
			// By default the pods of the configuration are ready
			names.Config: "startup,ready,live",

			// By default the second revision doesn't become ready
			secondRevisionName: "startup",
		},
	}
	cm, err := cmClient.Create(t.Context(), cm, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create create config map:", err)
	}

	test.EnsureCleanup(t, func() {
		cmClient.Delete(t.Context(), cm.Name, metav1.DeleteOptions{})
	})

	t.Log("Creating route")
	if _, err := v1test.CreateRoute(t, clients, names); err != nil {
		t.Fatal("Failed to create Route:", err)
	}

	t.Log("Creating configuration")
	cfg, err := v1test.CreateConfiguration(t, clients, names,
		withMinScale(minScale),
		// Make sure we scale down quickly after panic, before the autoscaler get killed by chaosduck.
		withWindow(scalingWindow),
		rtesting.WithConfigReadinessProbe(&corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz/ready",
					Port: intstr.FromInt(8080),
				},
			},
		}),
		rtesting.WithConfigVolume("state", "/etc/config", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cm.Name,
				},
			},
		}),
	)
	if err != nil {
		t.Fatal("Failed to create Configuration:", err)
	}

	firstRevName := latestRevisionName(t, clients, names.Config, "")
	firstRevServiceName := PrivateServiceName(t, clients, firstRevName)

	t.Log("Waiting for first revision to become ready")
	err = v1test.WaitForRevisionState(clients.ServingClient, firstRevName, v1test.IsRevisionReady, "RevisionIsReady")
	if err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", firstRevName, err)
	}

	t.Log("Holding revision at minScale after becoming ready")
	if lr, ok := ensureDesiredScale(clients, t, firstRevServiceName, gte(minScale)); !ok {
		t.Fatalf("The revision %q observed scale %d < %d after becoming ready", firstRevName, lr, minScale)
	}

	t.Log("Updating configuration")
	if _, err := v1test.PatchConfig(t, clients, cfg, rtesting.WithConfigEnv(corev1.EnvVar{Name: "FOO", Value: "BAR"})); err != nil {
		t.Fatal("Failed to update Configuration:", err)
	}

	t.Logf("Waiting for %v pods to be created", minScale)
	var podList *corev1.PodList

	err = wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(context.Context) (bool, error) {
		revLabel, err := labels.NewRequirement(serving.RevisionLabelKey, selection.Equals, []string{secondRevisionName})
		if err != nil {
			return false, fmt.Errorf("unable to create rev label: %w", err)
		}

		pods := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace)
		podList, err = pods.List(ctx, metav1.ListOptions{
			LabelSelector: revLabel.String(),
		})
		if err != nil {
			return false, err
		}

		runningPods := 0
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning {
				runningPods++
			}
		}

		return runningPods == minScale, nil
	})

	if errors.Is(err, context.DeadlineExceeded) {
		for _, pod := range podList.Items {
			t.Logf("pod %s is in phase %s", pod.Name, pod.Status.Phase)
		}
		t.Fatal("Timed out waiting for pods to be running", err)
	} else if err != nil {
		t.Fatal("Failed waiting for pods to be running", err)
	}

	podName := podList.Items[0].Name

	t.Logf("Marking revision %s pod %s as ready", secondRevisionName, podName)
	newCM := cm.DeepCopy()
	newCM.Data[podName] = "startup,ready"

	patchBytes, err := duck.CreateBytePatch(cm, newCM)
	if err != nil {
		t.Fatal("Failed to create patch:", err)
	}

	cm, err = cmClient.Patch(ctx, names.Config, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		t.Fatal("Failed to patch ConfigMap", err)
	}

	t.Logf("Waiting for scaling window %v to pass", scalingWindow)

	// Wait two autoscaling window durations
	// Scaling decisions are made at the end of the window
	time.Sleep(scalingWindow)

	t.Log("Check original revision holding at min scale", minScale)
	if _, ok := ensureDesiredScale(clients, t, firstRevServiceName, gte(minScale)); !ok {
		t.Fatalf("Revision %q was scaled down prematurely", firstRevName)
	}
}

func TestMinScale(t *testing.T) {
	t.Parallel()

	const minScale = 4

	clients := Setup(t)

	names := test.ResourceNames{
		// Config and Route have different names to avoid false positives
		Config: test.ObjectNameForTest(t),
		Route:  test.ObjectNameForTest(t),
		Image:  test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating route")
	if _, err := v1test.CreateRoute(t, clients, names); err != nil {
		t.Fatal("Failed to create Route:", err)
	}

	t.Log("Creating configuration")
	cfg, err := v1test.CreateConfiguration(t, clients, names, withMinScale(minScale),
		// Make sure we scale down quickly after panic, before the autoscaler get killed by chaosduck.
		withWindow(autoscaling.WindowMin),
		// Pass low resource requirements to avoid Pod scheduling problems
		// on busy clusters.  This is adapted from ./test/e2e/scale.go
		func(cfg *v1.Configuration) {
			cfg.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
			}
		})
	if err != nil {
		t.Fatal("Failed to create Configuration:", err)
	}

	revName := latestRevisionName(t, clients, names.Config, "")
	serviceName := PrivateServiceName(t, clients, revName)

	t.Log("Waiting for revision to scale to minScale before becoming ready")
	if lr, err := waitForDesiredScale(clients, serviceName, gte(minScale)); err != nil {
		t.Fatalf("The revision %q scaled to %d < %d before becoming ready: %v", revName, lr, minScale, err)
	}

	t.Log("Waiting for revision to become ready")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, revName, v1test.IsRevisionReady, "RevisionIsReady",
	); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", revName, err)
	}

	t.Log("Holding revision at minScale after becoming ready")
	if lr, ok := ensureDesiredScale(clients, t, serviceName, gte(minScale)); !ok {
		t.Fatalf("The revision %q observed scale %d < %d after becoming ready", revName, lr, minScale)
	}

	revision, err := clients.ServingClient.Revisions.Get(context.Background(), revName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("An error occurred getting revision %v, %v", revName, err)
	}
	if replicas := *revision.Status.ActualReplicas; replicas != minScale {
		t.Fatalf("Expected actual replicas for revision %v to be %v but got %v", revision.Name, minScale, replicas)
	}

	t.Log("Updating configuration")
	if _, err := v1test.PatchConfig(t, clients, cfg, rtesting.WithConfigEnv(corev1.EnvVar{Name: "FOO", Value: "BAR"})); err != nil {
		t.Fatal("Failed to update Configuration:", err)
	}

	newRevName := latestRevisionName(t, clients, names.Config, revName)
	newServiceName := PrivateServiceName(t, clients, newRevName)

	t.Log("Waiting for new revision to scale to minScale after update")
	if lr, err := waitForDesiredScale(clients, newServiceName, gte(minScale)); err != nil {
		t.Fatalf("The revision %q scaled to %d < %d after creating route: %v", newRevName, lr, minScale, err)
	}

	t.Log("Waiting for new revision to become ready")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, newRevName, v1test.IsRevisionReady, "RevisionIsReady",
	); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", newRevName, err)
	}

	t.Log("Holding new revision at minScale after becoming ready")
	if lr, ok := ensureDesiredScale(clients, t, newServiceName, gte(minScale)); !ok {
		t.Fatalf("The revision %q observed scale %d < %d after becoming ready", newRevName, lr, minScale)
	}

	revision, err = clients.ServingClient.Revisions.Get(context.Background(), newRevName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("An error occurred getting revision %v, %v", newRevName, err)
	}
	if replicas := *revision.Status.ActualReplicas; replicas != minScale {
		t.Fatalf("Expected actual replicas for revision %v to be %v but got %v", revision.Name, minScale, replicas)
	}

	t.Log("Waiting for old revision to scale below minScale after being replaced")
	if lr, err := waitForDesiredScale(clients, serviceName, lt(minScale)); err != nil {
		t.Fatalf("The revision %q scaled to %d > %d after not being routable anymore: %v", revName, lr, minScale, err)
	}

	t.Log("Deleting route", names.Route)
	if err := clients.ServingClient.Routes.Delete(context.Background(), names.Route, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete route %q: %v", names.Route, err)
	}

	t.Log("Waiting for new revision to scale below minScale when there is no route")
	if lr, err := waitForDesiredScale(clients, newServiceName, lt(minScale)); err != nil {
		t.Fatalf("The revision %q scaled to %d > %d after not being routable anymore: %v", newRevName, lr, minScale, err)
	}
}

func gte(m int) func(int) bool {
	return func(n int) bool {
		return n >= m
	}
}

func lt(m int) func(int) bool {
	return func(n int) bool {
		return n < m
	}
}

func withMinScale(minScale int) func(cfg *v1.Configuration) {
	return func(cfg *v1.Configuration) {
		if cfg.Spec.Template.Annotations == nil {
			cfg.Spec.Template.Annotations = make(map[string]string, 1)
		}
		cfg.Spec.Template.Annotations[autoscaling.MinScaleAnnotationKey] = strconv.Itoa(minScale)
	}
}

func withWindow(t time.Duration) func(cfg *v1.Configuration) {
	return func(cfg *v1.Configuration) {
		if cfg.Spec.Template.Annotations == nil {
			cfg.Spec.Template.Annotations = make(map[string]string, 1)
		}
		cfg.Spec.Template.Annotations[autoscaling.WindowAnnotationKey] = t.String()
	}
}

func latestRevisionName(t *testing.T, clients *test.Clients, configName, oldRevName string) string {
	// Wait for the Config have a LatestCreatedRevisionName
	if err := v1test.WaitForConfigurationState(
		clients.ServingClient, configName,
		func(c *v1.Configuration) (bool, error) {
			return c.Status.LatestCreatedRevisionName != oldRevName, nil
		}, "ConfigurationHasUpdatedCreatedRevision",
	); err != nil {
		t.Fatalf("The Configuration %q has not updated LatestCreatedRevisionName from %q: %v", configName, oldRevName, err)
	}

	config, err := clients.ServingClient.Configs.Get(context.Background(), configName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failed to get Configuration after it was seen to be live:", err)
	}

	return config.Status.LatestCreatedRevisionName
}

// waitForDesiredScale returns the last observed number of pods and/or error if the cond
// callback is never satisfied.
func waitForDesiredScale(clients *test.Clients, serviceName string, cond func(int) bool) (latestReady int, err error) {
	endpoints := clients.KubeClient.CoreV1().Endpoints(test.ServingFlags.TestNamespace)

	// See https://github.com/knative/serving/issues/7727#issuecomment-706772507 for context.
	return latestReady, wait.PollUntilContextTimeout(context.Background(), 250*time.Millisecond, 3*time.Minute, true, func(context.Context) (bool, error) {
		endpoint, err := endpoints.Get(context.Background(), serviceName, metav1.GetOptions{})
		if err != nil {
			return false, nil //nolint:nilerr
		}
		latestReady = resources.ReadyAddressCount(endpoint)
		return cond(latestReady), nil
	})
}

func ensureDesiredScale(clients *test.Clients, t *testing.T, serviceName string, cond func(int) bool) (latestReady int, observed bool) {
	endpoints := clients.KubeClient.CoreV1().Endpoints(test.ServingFlags.TestNamespace)

	err := wait.PollUntilContextTimeout(context.Background(), 250*time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) {
		endpoint, err := endpoints.Get(context.Background(), serviceName, metav1.GetOptions{})
		if err != nil {
			return false, nil //nolint:nilerr
		}

		if latestReady = resources.ReadyAddressCount(endpoint); !cond(latestReady) {
			return false, fmt.Errorf("scale %d didn't meet condition", latestReady)
		}

		return false, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Log("PollError =", err)
	}

	return latestReady, errors.Is(err, context.DeadlineExceeded)
}
