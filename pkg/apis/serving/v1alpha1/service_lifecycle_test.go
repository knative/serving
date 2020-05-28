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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	apistest "knative.dev/pkg/apis/testing"
	"knative.dev/pkg/ptr"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestServiceDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
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

func TestServiceGetConditionSet(t *testing.T) {
	s := &Service{}

	if got, want := s.GetConditionSet().GetTopLevelConditionType(), apis.ConditionReady; got != want {
		t.Errorf("GetTopLevelCondition=%v, want=%v", got, want)
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
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   "Foo",
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: ServiceConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
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
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
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
		s := Service{Status: tc.status}
		if e, a := tc.isReady, s.IsReady(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestServiceHappyPath(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Nothing from Configuration is nothing to us.
	svc.PropagateConfigurationStatus(&ConfigurationStatus{})
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Nothing from Route is nothing to us.
	svc.PropagateRouteStatus(&RouteStatus{})
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Done from Configuration moves our ConfigurationsReady condition
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	svc.MarkResourceNotConvertible(ConvertErrorf("manual", "something something not allowed.").(*CannotConvertError))
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)
	apistest.CheckConditionFailed(svc, ConditionTypeConvertible, t)

	// Done from Route moves our RoutesReady condition, which triggers us to be Ready.
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)

	// Check idempotency.
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)
}

func TestMarkRouteNotYetReady(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	svc.MarkRouteNotYetReady()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	dt := svc.GetCondition(ServiceConditionReady)
	if got, want := dt.Reason, trafficNotMigratedReason; got != want {
		t.Errorf("Condition Reason: got: %s, want: %s", got, want)
	}
	if got, want := dt.Message, trafficNotMigratedMessage; got != want {
		t.Errorf("Condition Message: got: %s, want: %s", got, want)
	}
}

func TestFailureRecovery(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Config failure causes us to become unready immediately (route still ok).
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Route failure causes route to become failed (config and service still failed).
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionRoutesReady, t)

	// Fix Configuration moves our ConfigurationsReady condition (route and service still failed).
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionRoutesReady, t)

	// Fix route, should make everything ready.
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)
}

func TestConfigurationFailurePropagation(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately.
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

}

func TestConfigurationFailureRecovery(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Done from Route moves our RoutesReady condition
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately (route still ok).
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)

	// Fixed the glitch.
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)
}

func TestConfigurationUnknownPropagation(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Configuration and Route become ready, making us ready.
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)

	// Configuration flipping back to Unknown causes us to become ongoing immediately
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	// Route is unaffected.
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)
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
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionRoutesReady, t)
}

func TestRouteFailureRecovery(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Done from Configuration moves our ConfigurationsReady condition
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Failure causes us to become unready immediately (config still ok).
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionRoutesReady, t)

	// Fixed the glitch.
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)
}

func TestRouteUnknownPropagation(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	// Configuration and Route become ready, making us ready.
	svc.PropagateConfigurationStatus(&ConfigurationStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(svc, ServiceConditionReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionSucceeded(svc, ServiceConditionRoutesReady, t)

	// Route flipping back to Unknown causes us to become ongoing immediately.
	svc.PropagateRouteStatus(&RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)
	// Configuration is unaffected.
	apistest.CheckConditionSucceeded(svc, ServiceConditionConfigurationsReady, t)
}

func TestServiceNotOwnedStuff(t *testing.T) {
	svc := &ServiceStatus{}
	svc.InitializeConditions()
	apistest.CheckConditionOngoing(svc, ServiceConditionReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionOngoing(svc, ServiceConditionRoutesReady, t)

	svc.MarkRouteNotOwned("mark")
	apistest.CheckConditionFailed(svc, ServiceConditionRoutesReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)

	svc.MarkConfigurationNotOwned("jon")
	apistest.CheckConditionFailed(svc, ServiceConditionConfigurationsReady, t)
	apistest.CheckConditionFailed(svc, ServiceConditionReady, t)
}

func TestRouteStatusPropagation(t *testing.T) {
	svc := &Service{}

	rsf := RouteStatusFields{
		URL: &apis.URL{
			Scheme: "http",
			Host:   "route.namespace.example.com",
		},
		Traffic: []TrafficTarget{{
			TrafficTarget: v1.TrafficTarget{
				Percent:      ptr.Int64(100),
				RevisionName: "newstuff",
			},
		}, {
			TrafficTarget: v1.TrafficTarget{
				Percent:      nil,
				RevisionName: "oldstuff",
			},
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
