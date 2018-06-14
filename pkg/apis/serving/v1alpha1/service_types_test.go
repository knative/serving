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

func TestServiceGeneration(t *testing.T) {
	service := Service{}
	if got, want := service.GetGeneration(), int64(0); got != want {
		t.Errorf("Empty Service generation should be %d, was %d", want, got)
	}

	answer := int64(42)
	service.SetGeneration(answer)
	if got := service.GetGeneration(); got != answer {
		t.Errorf("GetGeneration mismatch; got %d, want %d", got, answer)
	}
}

func TestServiceIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  ServiceStatus
		isReady bool
	}{
		{
			name:    "empty status should not be ready",
			status:  ServiceStatus{},
			isReady: false,
		},
		{
			name: "Different condition type should not be ready",
			status: ServiceStatus{
				Conditions: []ServiceCondition{
					{
						Type:   "Foo",
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: false,
		},
		{
			name: "False condition status should not be ready",
			status: ServiceStatus{
				Conditions: []ServiceCondition{
					{
						Type:   ServiceConditionReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			isReady: false,
		},
		{
			name: "Unknown condition status should not be ready",
			status: ServiceStatus{
				Conditions: []ServiceCondition{
					{
						Type:   ServiceConditionReady,
						Status: corev1.ConditionUnknown,
					},
				},
			},
			isReady: false,
		},
		{
			name: "Missing condition status should not be ready",
			status: ServiceStatus{
				Conditions: []ServiceCondition{
					{
						Type: ServiceConditionReady,
					},
				},
			},
			isReady: false,
		},
		{
			name: "True condition status should be ready",
			status: ServiceStatus{
				Conditions: []ServiceCondition{
					{
						Type:   ServiceConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: true,
		},
		{
			name: "Multiple conditions with ready status should be ready",
			status: ServiceStatus{
				Conditions: []ServiceCondition{
					{
						Type:   "Foo",
						Status: corev1.ConditionTrue,
					},
					{
						Type:   ServiceConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: true,
		},
		{
			name: "Multiple conditions with ready status false should not be ready",
			status: ServiceStatus{
				Conditions: []ServiceCondition{
					{
						Type:   "Foo",
						Status: corev1.ConditionTrue,
					},
					{
						Type:   ServiceConditionReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			isReady: false,
		},
	}

	for _, tc := range cases {
		if e, a := tc.isReady, tc.status.IsReady(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestServiceConditions(t *testing.T) {
	svc := &Service{}
	foo := &ServiceCondition{
		Type:   "Foo",
		Status: "True",
	}
	bar := &ServiceCondition{
		Type:   "Bar",
		Status: "True",
	}

	// Add a single condition.
	svc.Status.setCondition(foo)
	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove non-existent condition.
	svc.Status.RemoveCondition(bar.Type)
	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add a second Condition.
	svc.Status.setCondition(bar)
	if got, want := len(svc.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove the first Condition.
	svc.Status.RemoveCondition(foo.Type)
	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected condition length; got %d, want %d", got, want)
	}

	// Test Add nil condition.
	svc.Status.setCondition(nil)
	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatal("Error, nil condition was allowed to be added.")
	}
}

func TestTypicalServiceFlow(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRouteReady, t)

	// Nothing from Configuration is nothing to us.
	svc.Status.PropagateConfiguration(ConfigurationStatus{})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRouteReady, t)

	// Nothing from Route is nothing to us.
	svc.Status.PropagateRoute(RouteStatus{})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRouteReady, t)

	// Done from Configuration moves our ConfigurationReady condition
	svc.Status.PropagateConfiguration(ConfigurationStatus{
		Conditions: []ConfigurationCondition{{
			Type:   ConfigurationConditionReady,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRouteReady, t)

	// Done from Route moves our RouteReady condition, which triggers us to be Ready.
	svc.Status.PropagateRoute(RouteStatus{
		Conditions: []RouteCondition{{
			Type:   RouteConditionReady,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRouteReady, t)

	// Check idempotency
	svc.Status.PropagateRoute(RouteStatus{
		Conditions: []RouteCondition{{
			Type:   RouteConditionReady,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRouteReady, t)

	// Failure causes us to become unready immediately (config still ok).
	svc.Status.PropagateRoute(RouteStatus{
		Conditions: []RouteCondition{{
			Type:   RouteConditionReady,
			Status: corev1.ConditionFalse,
		}},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionRouteReady, t)

	// Fixed the glitch.
	svc.Status.PropagateRoute(RouteStatus{
		Conditions: []RouteCondition{{
			Type:   RouteConditionReady,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRouteReady, t)
}

func TestConfigurationFailurePropagation(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRouteReady, t)

	// Failure causes us to become unready immediately
	svc.Status.PropagateConfiguration(ConfigurationStatus{
		Conditions: []ConfigurationCondition{{
			Type:   ConfigurationConditionReady,
			Status: corev1.ConditionFalse,
		}},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRouteReady, t)
}

func TestRouteFailurePropagation(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRouteReady, t)

	// Failure causes us to become unready immediately
	svc.Status.PropagateRoute(RouteStatus{
		Conditions: []RouteCondition{{
			Type:   RouteConditionReady,
			Status: corev1.ConditionFalse,
		}},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionRouteReady, t)
}

func checkConditionSucceededService(rs ServiceStatus, rct ServiceConditionType, t *testing.T) *ServiceCondition {
	t.Helper()
	return checkConditionService(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedService(rs ServiceStatus, rct ServiceConditionType, t *testing.T) *ServiceCondition {
	t.Helper()
	return checkConditionService(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingService(rs ServiceStatus, rct ServiceConditionType, t *testing.T) *ServiceCondition {
	t.Helper()
	return checkConditionService(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionService(rs ServiceStatus, rct ServiceConditionType, cs corev1.ConditionStatus, t *testing.T) *ServiceCondition {
	t.Helper()
	r := rs.GetCondition(rct)
	if r == nil {
		t.Fatalf("Get(%v) = nil, wanted %v=%v", rct, rct, cs)
	}
	if r.Status != cs {
		t.Fatalf("Get(%v) = %v, wanted %v", rct, r.Status, cs)
	}
	return r
}
