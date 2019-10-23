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
	"testing"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
	v1a1testing "knative.dev/serving/pkg/testing/v1alpha1"
)

var (
	scaleToZeroOnDeployFalse = map[string]string{
		autoscaling.ScaleToZeroOnDeployAnnotation: strconv.FormatBool(false),
	}
	scaleToZeroOnDeployTrue = map[string]string{
		autoscaling.ScaleToZeroOnDeployAnnotation: strconv.FormatBool(true),
	}
)

// TestInitScaleZeroServiceLevel tests setting of annotation scaleToZeroOnDeploy on
// the revision level
func TestInitScaleZeroServiceLevel(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	defer test.TearDown(clients, names)

	updatedConfigAutoscalerCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-autoscaler")
	defer restoreCM(t, clients, updatedConfigAutoscalerCM)

	t.Log("Creating a new Service with scale to zero on deploy being true and verifying that no pods are created")
	createServiceAndCheckPods(t, clients, &names, false /* podsCreated */, v1a1testing.WithConfigAnnotations(scaleToZeroOnDeployTrue))

	names = test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	t.Log("Creating a new Service with scale to zero on deploy being false and verifying that pods are created")
	createServiceAndCheckPods(t, clients, &names, true /* podsCreated */, v1a1testing.WithConfigAnnotations(scaleToZeroOnDeployFalse))
}

// TestInitScaleZeroClusterLevel tests setting of scaleToZeroOnDeploy flags in both
// config-defaults and config-autoscaler on the cluster level.
func TestInitScaleZeroClusterLevel(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	tests := []struct {
		name                      string
		// configDefaultsFlag indicates if the scale to zero on deploy flag is set to true
		// or false in the config-defaults config map.
		configDefaultsFlag        bool
		// configAutoscalerFlag indicates if the scale to zero on deploy flag is set to true
		// or false in the config-autoscaler config map.
		configAutoscalerFlag      bool
		// serviceScaleToZeroUnset indicates whether pods will be created when the scale to zero
		// on deploy flag is unset on the test service created.
		serviceScaleToZeroUnset bool
		// serviceScaleToZeroFalse indicates whether pods will be created when the scale to zero
		// on deploy flag is set to false on the test service created.
		serviceScaleToZeroFalse bool
		// serviceScaleToZeroTrue indicates whether pods will be created when the scale to zero
		// on deploy flag is set to true on the test service created.
		serviceScaleToZeroTrue  bool
	}{{
		name: "both config-defaults config-autoscaler true",
		configDefaultsFlag: true,
		configAutoscalerFlag: true,
		serviceScaleToZeroUnset: false,
		serviceScaleToZeroFalse: true,
		serviceScaleToZeroTrue: false,
	}, {
		name: "config-autoscaler false but config-defaults true",
		configDefaultsFlag:        true,
		configAutoscalerFlag:      false,
		serviceScaleToZeroUnset: true,
		serviceScaleToZeroFalse: true,
		serviceScaleToZeroTrue:  true,
	}, {
		name: "config-defaults false but config-autoscaler true",
		configDefaultsFlag: false,
		configAutoscalerFlag: true,
		serviceScaleToZeroUnset: true,
		serviceScaleToZeroFalse: true,
		serviceScaleToZeroTrue: false,
	}, {
		name: "both config-defaults config-autoscaler false",
		configDefaultsFlag: false,
		configAutoscalerFlag: false,
		serviceScaleToZeroUnset: true,
		serviceScaleToZeroFalse: true,
		serviceScaleToZeroTrue: true,
	}}
	for _, tc := range tests {
		func() {
			t.Logf("Setting feature flag to %v on config-defaults ConfigMap and %v on config-autoscaler ConfigMap.", tc.configDefaultsFlag, tc.configAutoscalerFlag)
			if tc.configDefaultsFlag {
				updatedConfigDefaultsCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-defaults")
				defer restoreCM(t, clients, updatedConfigDefaultsCM)
			}
			if tc.configAutoscalerFlag {
				updatedConfigAutoscalerCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-autoscaler")
				defer restoreCM(t, clients, updatedConfigAutoscalerCM)
			}

			namesScaleToZeroUnset := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   "helloworld",
			}
			namesScaleToZeroFalse := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   "helloworld",
			}
			namesScaleToZeroTrue := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   "helloworld",
			}
			defer test.TearDown(clients, namesScaleToZeroUnset)
			t.Logf("Creating a new Service without scale to zero on deploy being set and verifying that podsCreated is %v.", tc.serviceScaleToZeroUnset)
			createServiceAndCheckPods(t, clients, &namesScaleToZeroUnset, tc.serviceScaleToZeroUnset /* podsCreated */)

			defer test.TearDown(clients, namesScaleToZeroFalse)
			t.Logf("Creating a new Service with scale to zero on deploy being false and verifying that podsCreated is %v.", tc.serviceScaleToZeroFalse)
			createServiceAndCheckPods(t, clients, &namesScaleToZeroFalse, tc.serviceScaleToZeroFalse /* podsCreated */, v1a1testing.WithConfigAnnotations(scaleToZeroOnDeployFalse))

			defer test.TearDown(clients, namesScaleToZeroTrue)
			t.Logf("Creating a new Service with check scale to zero on deploy being true and verifying that podsCreated is %v.", tc.serviceScaleToZeroTrue)
			createServiceAndCheckPods(t, clients, &namesScaleToZeroTrue, tc.serviceScaleToZeroTrue /* podsCreated */, v1a1testing.WithConfigAnnotations(scaleToZeroOnDeployTrue))
		}()
	}
}

// TestInitScaleZeroMinScaleClusterLevel tests setting of annotation scaleToZeroOnDeploy on
// the config-defaults and config-autoscaler with minScale > 0
func TestInitScaleZeroMinScaleClusterLevel(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	defer test.TearDown(clients, names)

	updatedConfigDefaultsCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-defaults")
	updatedConfigAutoscalerCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-autoscaler")
	defer restoreCM(t, clients, updatedConfigDefaultsCM)
	defer restoreCM(t, clients, updatedConfigAutoscalerCM)

	t.Log("Creating a new Service with minScale greater than 0 and verifying that pods are created.")
	createServiceAndCheckPods(t, clients, &names, true /* podsCreated */, v1a1testing.WithConfigAnnotations(map[string]string{
		autoscaling.MinScaleAnnotationKey: "1",
	}))
}

// TestInitScaleZeroMinScaleServiceLevel tests setting of annotation scaleToZeroOnDeploy on
// the service level with minScale > 0
func TestInitScaleZeroMinScaleServiceLevel(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	defer test.TearDown(clients, names)

	updatedConfigAutoscalerCM := setScaleToZeroOnDeployOnCluster(t, clients, "config-autoscaler")
	defer restoreCM(t, clients, updatedConfigAutoscalerCM)

	t.Log("Creating a new Service with scale to zero on deploy being true and minScale greater than 0; verify that pods are created")
	createServiceAndCheckPods(t, clients, &names, true /* podsCreated */, v1a1testing.WithConfigAnnotations(map[string]string{
		autoscaling.MinScaleAnnotationKey:         "1",
		autoscaling.ScaleToZeroOnDeployAnnotation: strconv.FormatBool(true),
	}))
}

func restoreCM(t *testing.T, clients *test.Clients, oldCM *v1.ConfigMap) {
	if _, err := patchCM(clients, oldCM); err != nil {
		t.Fatalf("Error restoring ConfigMap %q: %v", oldCM.Name, err)
	}
	t.Logf("Successfully restored ConfigMap %s", oldCM.Name)
}

func setScaleToZeroOnDeployOnCluster(t *testing.T, clients *test.Clients, configMapName string) *v1.ConfigMap {
	t.Logf("Fetch %s ConfigMap", configMapName)
	original, err := rawCM(clients, configMapName)
	if err != nil || original == nil {
		t.Fatalf("Error fetching %s: %v", configMapName, err)
	}
	var newCM *v1.ConfigMap
	original.Data["scale-to-zero-on-deploy"] = "true"
	if newCM, err = patchCM(clients, original); err != nil {
		t.Fatalf("Failed to update %s: %v", configMapName, err)
	}
	t.Logf("Successfully updated %s configMap.", configMapName)

	newCM.Data["scale-to-zero-on-deploy"] = "false"
	test.CleanupOnInterrupt(func() { restoreCM(t, clients, newCM) })
	return newCM
}

// Returns true if pods are created; false if otherwise.
func createServiceAndCheckPods(t *testing.T, clients *test.Clients, names *test.ResourceNames, podsCreated bool, fopt ...v1a1testing.ServiceOption) {
	t.Helper()
	test.CleanupOnInterrupt(func() {
		test.TearDown(clients, *names)
	})
	objects, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, names,
		false,
		fopt...)
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
	if gotPods := len(podList.Items); podsCreated != (gotPods > 0) {
		t.Fatalf("Expect podsCreated to be %v, but got %d pods.", podsCreated, gotPods)
	}
}
