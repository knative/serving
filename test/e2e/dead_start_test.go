//go:build e2e
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

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	v1options "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// withServiceImage sets the container image to be the provided string.
func withServiceImage(img string) v1options.ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Template.Spec.PodSpec.Containers[0].Image = img
	}
}

func withConfigImage(img string) rtesting.ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.Containers[0].Image = img
	}
}

func generationLabelSelector(config string, generation int) string {
	return fmt.Sprintf("%s=%s,%s=%s", serving.ConfigurationLabelKey, config, "serving.knative.dev/configurationGeneration", strconv.Itoa(generation))
}

func revisionLabelSelector(revName string) string {
	return fmt.Sprintf("%s=%s", serving.RevisionLabelKey, revName)
}

func IsRevisionRestarting(clients *test.Clients) func(r *v1.Revision) (bool, error) {
	return func(r *v1.Revision) (bool, error) {
		// serving.knative.dev / revision = dead - start - to - healthy - tbibclgi - 00001
		pods := clients.KubeClient.CoreV1().Pods(r.GetNamespace())
		podList, err := pods.List(context.Background(), metav1.ListOptions{
			LabelSelector: revisionLabelSelector(r.Name),
			FieldSelector: "status.phase!=Pending",
		})
		if err != nil {
			return false, err
		}

		// verify the pods are restarting.
		for i := range podList.Items {
			conds := podList.Items[i].Status.ContainerStatuses
			for j := range conds {
				if conds[j].RestartCount > 0 {
					return true, nil
				}
			}
		}
		return false, nil
	}
}

func IsRevisionScaledZero(clients *test.Clients) func(r *v1.Revision) (bool, error) {
	return func(r *v1.Revision) (bool, error) {
		// serving.knative.dev / revision = dead - start - to - healthy - tbibclgi - 00001
		pods := clients.KubeClient.CoreV1().Pods(r.GetNamespace())
		podList, err := pods.List(context.Background(), metav1.ListOptions{
			LabelSelector: revisionLabelSelector(r.Name),
			FieldSelector: "status.phase!=Pending",
		})
		if err != nil {
			return false, err
		}
		gotPods := len(podList.Items)

		return gotPods == 0, nil
	}
}

// func latestRevisionName(t *testing.T, clients *test.Clients, configName, oldRevName string) string {
// 	// Wait for the Config have a LatestCreatedRevisionName
// 	if err := v1test.WaitForConfigurationState(
// 		clients.ServingClient, configName,
// 		func(c *v1.Configuration) (bool, error) {
// 			return c.Status.LatestCreatedRevisionName != oldRevName, nil
// 		}, "ConfigurationHasUpdatedCreatedRevision",
// 	); err != nil {
// 		t.Fatalf("The Configuration %q has not updated LatestCreatedRevisionName from %q: %v", configName, oldRevName, err)
// 	}

// 	config, err := clients.ServingClient.Configs.Get(context.Background(), configName, metav1.GetOptions{})
// 	if err != nil {
// 		t.Fatal("Failed to get Configuration after it was seen to be live:", err)
// 	}

// 	return config.Status.LatestCreatedRevisionName
// }

// This test case creates a service which can never reach a ready state.
// The service is then udpated with a healthy image and is verified that
// the healthy revision is ready and the unhealhy revision is scaled to zero.
func TestDeadStartToHealthy(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	names := test.ResourceNames{
		Config:  test.ObjectNameForTest(t),
		Service: test.ObjectNameForTest(t),
		Route:   test.ObjectNameForTest(t),
		Image:   test.DeadStart,
	}
	test.EnsureTearDown(t, clients, &names)

	var err error
	const minScale = 3

	t.Log("Creating route")
	if _, err := v1test.CreateRoute(t, clients, names); err != nil {
		t.Fatal("Failed to create Route:", err)
	}
	cfg, err := v1test.CreateConfiguration(t, clients, names,
		withMinScale(minScale),
		// rtesting.WithConfigTEST(serving.ProgressDeadlineAnnotationKey, "1s"),
		rtesting.WithConfigRevisionTimeoutSeconds(1),
	)
	if err != nil {
		t.Fatal("Failed to create Configuration:", err)
	}

	// time.Sleep(time.Hour)
	failedRevName := latestRevisionName(t, clients, names.Config, "")

	t.Log("Waiting for revision to restart")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, failedRevName, IsRevisionRestarting(clients), "RevisionIsRestarting",
	); err != nil {
		t.Fatalf("The Revision %q is not restarting: %v", failedRevName, err)
	}

	t.Log("Updating configuration with valid image")
	if _, err := v1test.PatchConfig(t, clients, cfg, withConfigImage(pkgtest.ImagePath(test.HelloWorld))); err != nil {
		t.Fatal("Failed to update Configuration:", err)
	}

	healthyRevName := latestRevisionName(t, clients, names.Config, failedRevName)

	t.Log("Waiting for revision to become ready")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, healthyRevName, v1test.IsRevisionReady, "RevisionIsReady",
	); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", healthyRevName, err)
	}

	t.Log("Waiting for revision scale zero")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, failedRevName, IsRevisionScaledZero(clients), "RevisionIsScaledZero",
	); err != nil {
		t.Fatalf("The Revision %q is not restarting: %v", failedRevName, err)
	}

	t.Log("Verify revision Active is Unreachable")
	if err = v1test.CheckRevisionState(clients.ServingClient, failedRevName, func(r *v1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1.RevisionConditionActive)
		t.Logf("Revision %s Active state = %#v", failedRevName, cond)
		if cond != nil {
			if cond.Reason == "Unreachable" {
				return true, nil
			}
			return true, fmt.Errorf("The Revision %s Active condition has: (Reason=%q, Message=%q)",
				failedRevName, cond.Reason, cond.Message)
		}
		return false, fmt.Errorf("The Revision %s has empty Active condition", failedRevName)
	}); err != nil {
		t.Fatal("Failed to validate revision state:", err)
	}

}

// This test case updates a healthy service with an image that can never reach a ready state.
// The healthy revision remains Ready and the DeadStart revision doesnt not scale down until ProgressDeadline is reached.
func TestDeadStartFromHealthy(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	names := test.ResourceNames{
		Config:  test.ObjectNameForTest(t),
		Service: test.ObjectNameForTest(t),
		Route:   test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}
	test.EnsureTearDown(t, clients, &names)

	var err error
	const minScale = 3

	t.Log("Creating route")
	if _, err := v1test.CreateRoute(t, clients, names); err != nil {
		t.Fatal("Failed to create Route:", err)
	}

	cfg, err := v1test.CreateConfiguration(t, clients, names, withMinScale(minScale),
		// rtesting.WithConfigAnn(serving.ProgressDeadlineAnnotationKey, "45s"),
		rtesting.WithConfigRevisionTimeoutSeconds(1),
	)
	if err != nil {
		t.Fatal("Failed to create Configuration:", err)
	}

	healthyRevName := latestRevisionName(t, clients, names.Config, "")

	t.Log("Waiting for revision to become ready")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, healthyRevName, v1test.IsRevisionReady, "RevisionIsReady",
	); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", healthyRevName, err)
	}

	t.Log("Updating configuration with valid image")
	if _, err := v1test.PatchConfig(t, clients, cfg, withConfigImage(pkgtest.ImagePath(test.DeadStart))); err != nil {
		t.Fatal("Failed to update Configuration:", err)
	}

	failedRevName := latestRevisionName(t, clients, names.Config, healthyRevName)

	t.Log("Waiting for revision to restart")
	if err := v1test.WaitForRevisionState(
		clients.ServingClient, failedRevName, IsRevisionRestarting(clients), "RevisionIsRestarting",
	); err != nil {
		t.Fatalf("The Revision %q is not restarting: %v", failedRevName, err)
	}

	for i := 0; i < 100; i++ {
		fmt.Printf("healthyRevName %v failedRevName %v\n", healthyRevName, failedRevName)
	}

	// TODO: verify the restart is queued.
	// it only shutsdown once progressdeadline reached.
}
