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
	"testing"

	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1a1testing "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

var (
	initScale10 = map[string]string{
		autoscaling.InitialScaleAnnotationKey: "10",
	}
	initScale0 = map[string]string{
		autoscaling.InitialScaleAnnotationKey: "0",
	}
)

// TestInitScaleZeroServiceLevel tests setting of annotation scaleToZeroOnDeploy on
// the revision level. This test runs after the cluster wide flag allow-zero-initial-scale
// is set to true.
func TestInitScaleZeroServiceLevel(t *testing.T) {
	t.Skip()
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names0 := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names0) })
	defer test.TearDown(clients, names0)

	t.Log("Creating a new Service with initial scale zero and verifying that no pods are created")
	if _, err := v1a1test.CreateRunLatestServiceReadyWithNumPods(t, clients, &names0,
		0, v1a1testing.WithConfigAnnotations(initScale0)); err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names0.Service, err)
	}

	names10 := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names10) })
	defer test.TearDown(clients, names10)
	t.Log("Creating a new Service with scale to zero on deploy being false and verifying that pods are created")
	if _, err := v1a1test.CreateRunLatestServiceReadyWithNumPods(t, clients, &names10,
		10, v1a1testing.WithConfigAnnotations(initScale10)); err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names10.Service, err)
	}
}

// TestInitScaleZeroMinScaleServiceLevel tests setting of annotation scaleToZeroOnDeploy on
// the service level with minScale > 0. This test runs after the cluster wide flag allow-zero-initial-scale
// is set to true.
func TestInitScaleZeroMinScaleServiceLevel(t *testing.T) {
	t.Skip()
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service with scale to zero on deploy being true and minScale greater than 0; verify that one pod is created")
	if _, err := v1a1test.CreateRunLatestServiceReadyWithNumPods(t, clients, &names, 1, v1a1testing.WithConfigAnnotations(map[string]string{
		autoscaling.MinScaleAnnotationKey:     "1",
		autoscaling.InitialScaleAnnotationKey: "0",
	})); err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
}
