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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

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
	CreateAndVerifyInitialScaleConfiguration(t, clients, names, initialScale)

	t.Logf("Waiting for Configuration %q to scale back below initialScale", names.Config)
	if err := v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(s *v1.Configuration) (b bool, e error) {
		pods := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace)
		podList, err := pods.List(context.Background(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", serving.ConfigurationLabelKey, names.Config),
			FieldSelector: "status.phase!=Terminating",
		})

		return len(podList.Items) < initialScale, err
	}, "ConfigurationScaledBelowInitial"); err != nil {
		t.Fatal("Configuration did not scale back below initialScale:", err)
	}
}
