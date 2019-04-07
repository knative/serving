/*
Copyright 2018 The Knative Authors

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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestServiceDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1beta1.Conditions{},
	}, {
		name: "legacy targetable",
		t:    &duckv1alpha1.LegacyTargetable{},
	}, {
		name: "addressable",
		t:    &duckv1alpha1.Addressable{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Service{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Service, %T) = %v", test.t, err)
			}
		})
	}
}

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
	}{{
		name:    "empty status should not be ready",
		status:  ServiceStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: ServiceStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   "Foo",
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: ServiceStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: ServiceStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: ServiceStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type: ServiceConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: ServiceStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: ServiceStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   "Foo",
					Status: corev1.ConditionTrue,
				}, {
					Type:   ServiceConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: ServiceStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   "Foo",
					Status: corev1.ConditionTrue,
				}, {
					Type:   ServiceConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}}

	for _, tc := range cases {
		if e, a := tc.isReady, tc.status.IsReady(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestServiceHappyPath(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Nothing from Configuration is nothing to us.
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Nothing from Route is nothing to us.
	svc.Status.PropagateRouteStatus(&RouteStatus{})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Done from Configuration moves our ConfigurationsReady condition
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Done from Route moves our RoutesReady condition, which triggers us to be Ready.
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)

	// Check idempotency
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)
}

func TestMarkRouteNotYetReady(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)
	svc.Status.MarkRouteNotYetReady()
	dt := checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	if dt == nil {
		t.Fatal("ServiceConditionReady was nil")
	}
	if got, want := dt.Reason, trafficNotMigratedReason; got != want {
		t.Errorf("Condition Reason: got: %s, want: %s", got, want)
	}
	if got, want := dt.Message, trafficNotMigratedMessage; got != want {
		t.Errorf("Condition Message: got: %s, want: %s", got, want)
	}
}

func TestFailureRecovery(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Config failure causes us to become unready immediately (route still ok).
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Route failure causes route to become failed (config and service still failed).
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionRoutesReady, t)

	// Fix Configuration moves our ConfigurationsReady condition (route and service still failed).
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionRoutesReady, t)

	// Fix route, should make everything ready.
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)
}

func TestConfigurationFailurePropagation(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)
}

func TestConfigurationFailureRecovery(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Done from Route moves our RoutesReady condition
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately (route still ok).
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)

	// Fixed the glitch.
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)
}

func TestConfigurationUnknownPropagation(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Configuration and Route become ready, making us ready.
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)

	// Configuration flipping back to Unknown causes us to become ongoing immediately
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	// Route is unaffected.
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)
}

func TestSetManualStatus(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Status should remain unknown
	svc.Status.SetManualStatus()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Going back from manual will result in propagation to reoccur, and should make us ready
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)

	// Going back to unknown should make us unknown again
	svc.Status.SetManualStatus()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

}

func TestConfigurationStatusPropagation(t *testing.T) {
	svc := &Service{}

	csf := ConfigurationStatusFields{
		LatestReadyRevisionName:   "foo",
		LatestCreatedRevisionName: "bar",
	}
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		ConfigurationStatusFields: csf,
	})

	want := ServiceStatus{
		ConfigurationStatusFields: csf,
	}

	if diff := cmp.Diff(want, svc.Status); diff != "" {
		t.Errorf("unexpected ServiceStatus (-want +got): %s", diff)
	}
}

func TestRouteFailurePropagation(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionRoutesReady, t)
}

func TestRouteFailureRecovery(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Done from Configuration moves our ConfigurationsReady condition
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately (config still ok).
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	checkConditionFailedService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionFailedService(svc.Status, ServiceConditionRoutesReady, t)

	// Fixed the glitch.
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)
}

func TestRouteUnknownPropagation(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	// Configuration and Route become ready, making us ready.
	svc.Status.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	checkConditionSucceededService(svc.Status, ServiceConditionReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionSucceededService(svc.Status, ServiceConditionRoutesReady, t)

	// Route flipping back to Unknown causes us to become ongoing immediately.
	svc.Status.PropagateRouteStatus(&RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)
	// Configuration is unaffected.
	checkConditionSucceededService(svc.Status, ServiceConditionConfigurationsReady, t)
}

func TestServiceNotOwnedStuff(t *testing.T) {
	svc := &Service{}
	svc.Status.InitializeConditions()
	checkConditionOngoingService(svc.Status, ServiceConditionReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionConfigurationsReady, t)
	checkConditionOngoingService(svc.Status, ServiceConditionRoutesReady, t)

	want := "NotOwned"
	svc.Status.MarkRouteNotOwned("mark")
	if got := checkConditionFailedService(svc.Status, ServiceConditionRoutesReady, t); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %v, want %v", got, want)
	}
	if got := checkConditionFailedService(svc.Status, ServiceConditionReady, t); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %v, want %v", got, want)
	}

	svc.Status.MarkConfigurationNotOwned("jon")
	if got := checkConditionFailedService(svc.Status, ServiceConditionConfigurationsReady, t); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %v, want %v", got, want)
	}
	if got := checkConditionFailedService(svc.Status, ServiceConditionReady, t); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %v, want %v", got, want)
	}
}

func TestRouteStatusPropagation(t *testing.T) {
	svc := &Service{}

	rsf := RouteStatusFields{
		Domain: "example.com",
		Traffic: []TrafficTarget{{
			Percent:      100,
			RevisionName: "newstuff",
		}, {
			Percent:      0,
			RevisionName: "oldstuff",
		}},
	}

	svc.Status.PropagateRouteStatus(&RouteStatus{
		RouteStatusFields: rsf,
	})

	want := ServiceStatus{
		RouteStatusFields: rsf,
	}

	if diff := cmp.Diff(want, svc.Status); diff != "" {
		t.Errorf("unexpected ServiceStatus (-want +got): %s", diff)
	}
}

func checkConditionSucceededService(rs ServiceStatus, rct apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkConditionService(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedService(rs ServiceStatus, rct apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkConditionService(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingService(rs ServiceStatus, rct apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkConditionService(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionService(rs ServiceStatus, rct apis.ConditionType, cs corev1.ConditionStatus, t *testing.T) *apis.Condition {
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

func TestServiceGetGroupVersionKind(t *testing.T) {
	s := &Service{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1alpha1",
		Kind:    "Service",
	}
	if got := s.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestAnnotateUserInfo(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	withUserAnns := func(u1, u2 string, s *Service) *Service {
		a := s.GetAnnotations()
		if a == nil {
			a = map[string]string{}
			defer s.SetAnnotations(a)
		}
		a[CreatorAnnotation] = u1
		a[UpdaterAnnotation] = u2
		return s
	}
	tests := []struct {
		name     string
		user     string
		this     *Service
		prev     *Service
		wantAnns map[string]string
	}{{
		"create-new", u1, &Service{}, nil,
		map[string]string{
			CreatorAnnotation: u1,
			UpdaterAnnotation: u1,
		},
	}, {
		// Old objects don't have the annotation, and unless there's a change in
		// data they won't get it.
		"update-no-diff-old-object", u1, &Service{}, &Service{},
		map[string]string{},
	}, {
		"update-no-diff-new-object", u2,
		withUserAnns(u1, u1, &Service{}),
		withUserAnns(u1, u1, &Service{}),
		map[string]string{
			CreatorAnnotation: u1,
			UpdaterAnnotation: u1,
		},
	}, {
		"update-diff-old-object", u2,
		&Service{Spec: ServiceSpec{Release: &ReleaseType{}}},
		&Service{Spec: ServiceSpec{RunLatest: &RunLatestType{}}},
		map[string]string{
			UpdaterAnnotation: u2,
		},
	}, {
		"update-diff-new-object", u3,
		withUserAnns(u1, u2, &Service{Spec: ServiceSpec{Release: &ReleaseType{}}}),
		withUserAnns(u1, u2, &Service{Spec: ServiceSpec{RunLatest: &RunLatestType{}}}),
		map[string]string{
			CreatorAnnotation: u1,
			UpdaterAnnotation: u3,
		},
	}}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.this.AnnotateUserInfo(context.Background(), test.prev, &authv1.UserInfo{
				Username: test.user,
			})
			if got, want := test.this.GetAnnotations(), test.wantAnns; !cmp.Equal(got, want) {
				t.Errorf("Annotations = %v, want: %v, diff (-got, +want): %s", got, want, cmp.Diff(got, want))
			}
		})
	}

}
