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
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	v1testing "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestInitScaleZero tests setting of annotation initialScale to 0 on
// the revision level. This test runs after the cluster wide flag allow-zero-initial-scale
// is set to true.
func TestInitScaleZero(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service with initial scale zero and verifying that no pods are created")
	createAndVerifyInitialScaleService(t, clients, names, 0)
}

// TestInitScalePositive tests setting of annotation initialScale to greater than 0 on
// the revision level.
func TestInitScalePositive(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service with initialScale 3 and verifying that pods are created")
	createAndVerifyInitialScaleService(t, clients, names, 3)
}

func createAndVerifyInitialScaleService(t *testing.T, clients *test.Clients, names test.ResourceNames, wantPods int) {
	t.Log("Creating a new Service.", "service", names.Service)
	_, err := v1test.CreateService(t, clients, names,
		v1testing.WithConfigAnnotations(map[string]string{
			autoscaling.InitialScaleAnnotationKey: strconv.Itoa(wantPods),
		}))
	if err != nil {
		t.Fatal("Failed creating initial service:", err)
	}

	t.Logf("Waiting for Service %q to transition to Ready with %d number of pods.", names.Service, wantPods)
	selector := fmt.Sprintf("%s=%s", serving.ConfigurationLabelKey, names.Service)
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1.Service) (b bool, e error) {
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
	}, "ServiceIsReadyWithWantPods"); err != nil {
		t.Fatal("Service does not have the desired number of pods running:", err)
	}
}
