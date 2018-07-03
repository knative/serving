/*
Copyright 2018 The Knative Authors.
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

func TestRouteGeneration(t *testing.T) {
	r := Route{}
	if a := r.GetGeneration(); a != 0 {
		t.Errorf("empty route generation should be 0 was: %d", a)
	}

	r.SetGeneration(5)
	if e, a := int64(5), r.GetGeneration(); e != a {
		t.Errorf("getgeneration mismatch expected: %d got: %d", e, a)
	}
}

func TestRouteIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  RouteStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  RouteStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: RouteStatus{
			Conditions: []RouteCondition{{
				Type:   RouteConditionAllTrafficAssigned,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: RouteStatus{
			Conditions: []RouteCondition{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: RouteStatus{
			Conditions: []RouteCondition{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: RouteStatus{
			Conditions: []RouteCondition{{
				Type: RouteConditionReady,
			}},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: RouteStatus{
			Conditions: []RouteCondition{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: RouteStatus{
			Conditions: []RouteCondition{{
				Type:   RouteConditionAllTrafficAssigned,
				Status: corev1.ConditionTrue,
			}, {
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: RouteStatus{
			Conditions: []RouteCondition{{
				Type:   RouteConditionAllTrafficAssigned,
				Status: corev1.ConditionTrue,
			}, {
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e, a := tc.isReady, tc.status.IsReady(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}
		})
	}
}

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

func TestTypicalRouteFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkTrafficAssigned()
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)

	// Verify that this doesn't reset our conditions.
	r.Status.InitializeConditions()
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)
}

func TestTrafficNotAssignedFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkTrafficNotAssigned("Revision", "does-not-exist")
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
