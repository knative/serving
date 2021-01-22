// +build e2e

/*
Copyright 2021 The Knative Authors

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

	pkgtest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
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
		testingv1.WithServiceAnnotation(serving.RolloutDurationKey, "60s"))
	if err != nil {
		t.Fatal("Create Ready Service:", err)
	}

	if _, err := v1test.PatchService(t, clients, robjs.Service, testingv1.WithServiceImage(pkgtest.ImagePath(test.Autoscale))); err != nil {
		t.Fatalf("Patch update for Service %s with image %s failed: %v", names.Service, test.Autoscale, err)
	}

	sawRolloutStatus := false
	sawTwoTrafficTargets := false

	// This will cover all the status checks:
	// - Status is in Rollout
	// - Traffic is splitting
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service,
		func(s *v1.Service) (bool, error) {
			// New revision not yet created.
			if s.Status.LatestCreatedRevisionName == robjs.Revision.Name {
				return false, nil
			}

			// Rollout not yet started.
			if !sawRolloutStatus && s.Status.GetCondition(v1.ServiceConditionRoutesReady).GetReason() != "RolloutInProgress" {
				return false, nil
			}

			// Rollout started!
			if !sawRolloutStatus {
				t.Log("Rollout started to the second revision")
				sawRolloutStatus = true
			}

			// Here we saw rollout. So two things can happen:
			// 1. Rollout finished
			// 2. Rollout in progress.

			// We must see two traffic targets at least once (60s to rollout, so more than once in reality).
			traffic := s.Status.Traffic
			if sawTwoTrafficTargets && s.IsReady() {
				t.Log("Saw the status ready!")
				// Rollout finished! Verify single target.
				if len(traffic) != 1 {
					return false, fmt.Errorf("expected one item in traffic stanza but got: %#v", traffic)
				}
				// Verify we reconciled on the second revision.
				if traffic[0].RevisionName != s.Status.LatestCreatedRevisionName {
					return false, fmt.Errorf("expected %s as final targtet revision but got %s", s.Status.LatestCreatedRevisionName, traffic[0].RevisionName)
				}
				// All good, we're done.
				return true, nil
			}

			// Verify traffic shape. During the rollout there should be exactly 2 items or rollout just finished
			// and the new target has 100%, but service is not yet ready.
			if tl := len(traffic); tl != 2 {
				if sawTwoTrafficTargets && tl == 1 &&
					traffic[0].RevisionName == s.Status.LatestCreatedRevisionName && *traffic[0].Percent == 100 {
					return false, nil
				}
				return false, fmt.Errorf("Expected two items in traffic stanza but got: %#v", s.Status.Traffic)
			} else if !sawTwoTrafficTargets {
				t.Log("Saw two traffic targets")
				sawTwoTrafficTargets = true
			}

			return false, nil
		}, "ServiceIsRollingOut"); err != nil {
		t.Fatalf("Failed waiting for Rollout %q to complete: %+v", names.Service, err)
	}
}
