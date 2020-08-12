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
	"fmt"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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
		Image:  "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating route")
	if _, err := v1test.CreateRoute(t, clients, names); err != nil {
		t.Fatal("Failed to create Route:", err)
	}

	t.Log("Creating configuration")
	cfg, err := v1test.CreateConfiguration(t, clients, names, withMinScale(minScale))
	if err != nil {
		t.Fatal("Failed to create Configuration:", err)
	}

	revName := latestRevisionName(t, clients, names.Config, "")
	serviceName := privateServiceName(t, clients, revName)

	t.Log("Waiting for revision to scale to minScale before becoming ready")
	if err := waitForDesiredScale(clients, serviceName, gte(minScale)); err != nil {
		t.Fatalf("The revision %q did not scale >= %d before becoming ready: %v", revName, minScale, err)
	}

	t.Log("Waiting for revision to become ready")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, revName, v1test.IsRevisionReady, "RevisionIsReady",
	); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", revName, err)
	}

	t.Log("Holding revision at minScale after becoming ready")
	if !ensureDesiredScale(clients, serviceName, gte(minScale)) {
		t.Fatalf("The revision %q did not stay at scale >= %d after becoming ready", revName, minScale)
	}

	t.Log("Updating configuration")
	if _, err := v1test.PatchConfig(t, clients, cfg, rtesting.WithConfigEnv(corev1.EnvVar{Name: "FOO", Value: "BAR"})); err != nil {
		t.Fatal("Failed to update Configuration:", err)
	}

	newRevName := latestRevisionName(t, clients, names.Config, revName)
	newServiceName := privateServiceName(t, clients, newRevName)

	t.Log("Waiting for new revision to scale to minScale after update")
	if err := waitForDesiredScale(clients, newServiceName, gte(minScale)); err != nil {
		t.Fatalf("The revision %q did not scale >= %d after creating route: %v", newRevName, minScale, err)
	}

	t.Log("Waiting for new revision to become ready")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, newRevName, v1test.IsRevisionReady, "RevisionIsReady",
	); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", newRevName, err)
	}

	t.Log("Holding new revision at minScale after becoming ready")
	if !ensureDesiredScale(clients, newServiceName, gte(minScale)) {
		t.Fatalf("The new revision %q did not stay at scale >= %d after becoming ready", newRevName, minScale)
	}

	t.Log("Waiting for old revision to scale below minScale after being replaced")
	if err := waitForDesiredScale(clients, serviceName, lt(minScale)); err != nil {
		t.Fatalf("The revision %q did not scale < minScale after being replaced: %v", revName, err)
	}

	t.Log("Deleting route")
	if err := clients.ServingClient.Routes.Delete(names.Route, &metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete route %q: %v", names.Route, err)
	}

	t.Log("Waiting for new revision to scale below minScale when there is no route")
	if err := waitForDesiredScale(clients, newServiceName, lt(minScale)); err != nil {
		t.Fatalf("The revision %q did not scale < minScale after being replaced: %v", newRevName, err)
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

	config, err := clients.ServingClient.Configs.Get(configName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failed to get Configuration after it was seen to be live:", err)
	}

	return config.Status.LatestCreatedRevisionName
}

func privateServiceName(t *testing.T, clients *test.Clients, revisionName string) string {
	var privateServiceName string

	if err := wait.PollImmediate(time.Second, 1*time.Minute, func() (bool, error) {
		sks, err := clients.NetworkingClient.ServerlessServices.Get(revisionName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		privateServiceName = sks.Status.PrivateServiceName
		return privateServiceName != "", nil
	}); err != nil {
		t.Fatalf("Error retrieving sks %q: %v", revisionName, err)
	}

	return privateServiceName
}

func waitForDesiredScale(clients *test.Clients, serviceName string, cond func(int) bool) error {
	endpoints := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace)

	return wait.PollImmediate(250*time.Millisecond, 1*time.Minute, func() (bool, error) {
		endpoint, err := endpoints.Get(serviceName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return cond(resources.ReadyAddressCount(endpoint)), nil
	})
}

func ensureDesiredScale(clients *test.Clients, serviceName string, cond func(int) bool) bool {
	endpoints := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace)

	return wait.PollImmediate(250*time.Millisecond, 5*time.Second, func() (bool, error) {
		endpoint, err := endpoints.Get(serviceName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		if scale := resources.ReadyAddressCount(endpoint); !cond(scale) {
			return false, fmt.Errorf("scale %d didn't meet condition", scale)
		}

		return false, nil
	}) == wait.ErrWaitTimeout
}
