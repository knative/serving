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

package initscalezero

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1a1testing "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

func TestInitScaleZeroClusterLevel(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := e2e.Setup(t)
	tests := []struct {
		name string
		// configAutoscalerFlag indicates if the scale to zero on deploy flag is set to true
		// or false in the config-autoscaler config map.
		configAutoscalerFlag bool
	}{{
		name:                 "config-autoscaler true",
		configAutoscalerFlag: true,
	}, {
		name:                 "config-autoscaler false",
		configAutoscalerFlag: false,
	}}
	for _, tc := range tests {
		func() {
			t.Logf("Setting flag to %v on config-autoscaler ConfigMap.", tc.configAutoscalerFlag)
			if tc.configAutoscalerFlag {
				updatedConfigAutoscalerCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-autoscaler")
				defer restoreCM(t, clients, updatedConfigAutoscalerCM)
			}
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   "helloworld",
			}
			defer test.TearDown(clients, names)
			t.Logf("Creating a new Service and verifying that scale to zero is %v.", tc.configAutoscalerFlag)
			createServiceAndCheckPods(t, clients, &names, tc.configAutoscalerFlag /* scaledToZero */)
		}()
	}
}

// TestInitScaleZeroMinScaleClusterLevel tests setting of scaleToZeroOnDeploy on
// config-autoscaler with minScale > 0
func TestInitScaleZeroMinScaleClusterLevel(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	defer test.TearDown(clients, names)

	updatedConfigAutoscalerCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-autoscaler")
	defer restoreCM(t, clients, updatedConfigAutoscalerCM)

	t.Log("Creating a new Service with minScale greater than 0 and verifying that pods are created.")
	createServiceAndCheckPods(t, clients, &names, false,
		v1a1testing.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey: "1",
		}))
}

func restoreCM(t *testing.T, clients *test.Clients, oldCM *v1.ConfigMap) {
	if _, err := e2e.PatchCM(clients, oldCM); err != nil {
		t.Fatalf("Error restoring ConfigMap %q: %v", oldCM.Name, err)
	}
	t.Logf("Successfully restored ConfigMap %s", oldCM.Name)
}

func setScaleToZeroOnDeployOnCluster(t *testing.T, clients *test.Clients, configMapName string) *v1.ConfigMap {
	t.Logf("Fetch %s ConfigMap", configMapName)
	original, err := e2e.RawCM(clients, configMapName)
	if err != nil || original == nil {
		t.Fatalf("Error fetching %s: %v", configMapName, err)
	}
	var newCM *v1.ConfigMap
	original.Data["scale-to-zero-on-deploy"] = "true"
	if newCM, err = e2e.PatchCM(clients, original); err != nil {
		t.Fatalf("Failed to update %s: %v", configMapName, err)
	}
	t.Logf("Successfully updated %s configMap.", configMapName)

	newCM.Data["scale-to-zero-on-deploy"] = "false"
	test.CleanupOnInterrupt(func() { restoreCM(t, clients, newCM) })
	return newCM
}

// Returns true if no pods are created; false if otherwise.
func createServiceAndCheckPods(t *testing.T, clients *test.Clients, names *test.ResourceNames, scaledToZero bool, fopt ...v1a1testing.ServiceOption) {
	t.Helper()
	test.CleanupOnInterrupt(func() {
		test.TearDown(clients, *names)
	})
	objects, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, names, test.ServingFlags.Https, fopt...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	pods := clients.KubeClient.Kube.CoreV1().Pods(test.ServingNamespace)
	podList, err := pods.List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", serving.RevisionLabelKey, objects.Revision.Name),
	})
	if err != nil {
		t.Fatalf("Failed to list all pods: %v: %v", names.Service, err)
	}
	if gotPods := len(podList.Items); scaledToZero != (gotPods == 0) {
		t.Fatalf("Expect scaledToZero to be %v, but got %d pods.", scaledToZero, gotPods)
	}
}
