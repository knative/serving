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
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

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
			return false, nil
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
			return false, nil
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
