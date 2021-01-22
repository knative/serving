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
	"testing"

	"knative.dev/serving/pkg/apis/serving"
	testingv1 "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestGradualRollout ensures the traffic is rolled out gradually.
func TestGradualRollout(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	// Clean up on test failure or interrupt
	test.EnsureTearDown(t, clients, &names)

	// Setup Service
	t.Log("Creating a new Service", names.Service)
	robjs, err := v1test.CreateServiceReady(t, clients, &names,
		testingv1.WithServiceAnnotation(serving.RolloutDurationKey, "150s"))
	if err != nil {
		t.Fatal("Create Ready Service:", err)
	}

	if _, err := v1test.PatchService(t, clients, robjs.Service, testingv1.WithServiceImage(test.Autoscale)); err != nil {
		t.Fatalf("Patch update for Service %s with image %s failed: %v", names.Service, test.Autoscale, err)
	}
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Failed waiting for Service %q to transition to Ready == True: %#v", names.Service, err)
	}
}
