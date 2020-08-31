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
	"fmt"
	"strconv"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestInitScaleZero tests setting of annotation initialScale to 0 on
// the revision level. This test runs after the cluster wide flag allow-zero-initial-scale
// is set to true.
func TestInitScaleZero(t *testing.T) {
	t.Parallel()

	clients := Setup(t)
	names := test.ResourceNames{
		Config: test.ObjectNameForTest(t),
		Image:  "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Configuration with initial scale zero and verifying that no pods are created")
	createAndVerifyInitialScaleConfiguration(t, clients, names, 0)
}

// TestInitScalePositive tests setting of annotation initialScale to greater than 0 on
// the revision level.
func TestInitScalePositive(t *testing.T) {
	t.Parallel()

	clients := Setup(t)
	names := test.ResourceNames{
		Config: test.ObjectNameForTest(t),
		Image:  "helloworld",
	}
	test.EnsureTearDown(t, clients, &names)

	const initialScale = 3
	t.Logf("Creating a new Configuration with initialScale %d and verifying that pods are created", initialScale)
	createAndVerifyInitialScaleConfiguration(t, clients, names, initialScale)

	t.Logf("Waiting for Configuration %q to scale back below initialScale", names.Config)
	if err := v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(s *v1.Configuration) (b bool, e error) {
		pods := clients.KubeClient.Kube.CoreV1().Pods(test.ServingNamespace)
		podList, err := pods.List(metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", serving.ConfigurationLabelKey, names.Config),
			FieldSelector: "status.phase!=Terminating",
		})

		return len(podList.Items) < initialScale, err
	}, "ConfigurationScaledBelowInitial"); err != nil {
		t.Fatal("Configuration did not scale back below initialScale:", err)
	}
}

func createAndVerifyInitialScaleConfiguration(t *testing.T, clients *test.Clients, names test.ResourceNames, wantPods int) {
	t.Log("Creating a new Configuration.", "configuration", names.Config)
	_, err := v1test.CreateConfiguration(t, clients, names, func(configuration *v1.Configuration) {
		configuration.Spec.Template.Annotations = kmeta.UnionMaps(
			configuration.Spec.Template.Annotations, map[string]string{
				autoscaling.InitialScaleAnnotationKey: strconv.Itoa(wantPods),
			})
	})
	if err != nil {
		t.Fatal("Failed creating initial configuration:", err)
	}

	t.Logf("Waiting for Configuration %q to transition to Ready with %d number of pods.", names.Config, wantPods)
	selector := fmt.Sprintf("%s=%s", serving.ConfigurationLabelKey, names.Config)
	if err := v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(s *v1.Configuration) (b bool, e error) {
		pods := clients.KubeClient.Kube.CoreV1().Pods(test.ServingNamespace)
		podList, err := pods.List(metav1.ListOptions{
			LabelSelector: selector,
			// Include both running and terminating pods, because we will scale down from
			// initial scale immediately if there's no traffic coming in.
			FieldSelector: "status.phase!=Pending",
		})
		if err != nil {
			return false, err
		}
		gotPods := len(podList.Items)
		if gotPods == wantPods {
			return s.IsReady(), nil
		}
		if gotPods > wantPods {
			return false, fmt.Errorf("expected %d pods created, got %d", wantPods, gotPods)
		}
		return false, nil
	}, "ConfigurationIsReadyWithWantPods"); err != nil {
		t.Fatal("Configuration does not have the desired number of pods running:", err)
	}
}
