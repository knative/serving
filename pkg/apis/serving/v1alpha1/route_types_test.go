/*
Copyright 2018 Google LLC. All rights reserved.
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
package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestRouteConditions(t *testing.T) {
	svc := &Route{}
	foo := &RouteCondition{
		Type:   "Foo",
		Status: "True",
	}
	bar := &RouteCondition{
		Type:   "Bar",
		Status: "True",
	}

	// Add a new condition.
	svc.Status.setCondition(foo)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nothing
	svc.Status.setCondition(nil)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove a non-existent condition.
	svc.Status.RemoveCondition(bar.Type)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add a second condition.
	svc.Status.setCondition(bar)

	if got, want := len(svc.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove an existing condition.
	svc.Status.RemoveCondition(bar.Type)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nil condition.
	svc.Status.setCondition(nil)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}
}

func TestTypicalFlowIngressFirst(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkIngressReady()
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkTrafficAssigned()
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)

	// Verify that this doesn't reset our conditions.
	r.Status.InitializeConditions()
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)
}

func TestTypicalFlowIngressLast(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkTrafficAssigned()
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkIngressReady()
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)
}

func TestTrafficNotAssignedFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkTrafficNotAssigned("Revision", "does-not-exist")
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionFailedRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionFailedRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkIngressReady()
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionFailedRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionFailedRoute(r.Status, RouteConditionReady, t)
}

func checkConditionSucceededRoute(rs RouteStatus, rct RouteConditionType, t *testing.T) {
	t.Helper()
	checkConditionRoute(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedRoute(rs RouteStatus, rct RouteConditionType, t *testing.T) {
	t.Helper()
	checkConditionRoute(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingRoute(rs RouteStatus, rct RouteConditionType, t *testing.T) {
	t.Helper()
	checkConditionRoute(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionRoute(rs RouteStatus, rct RouteConditionType, cs corev1.ConditionStatus, t *testing.T) {
	t.Helper()
	r := rs.GetCondition(rct)
	if r == nil {
		t.Fatalf("Get(%v) = nil, wanted %v=%v", rct, rct, cs)
	}
	if r.Status != cs {
		t.Fatalf("Get(%v) = %v, wanted %v", rct, r.Status, cs)
	}
}
