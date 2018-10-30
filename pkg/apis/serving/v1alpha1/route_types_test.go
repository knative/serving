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

	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestRouteDuckTypes(t *testing.T) {
	var emptyGen duckv1alpha1.Generation
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "generation",
		t:    &emptyGen,
	}, {
		name: "conditions",
		t:    &duckv1alpha1.Conditions{},
	}, {
		name: "legacy targetable",
		t:    &duckv1alpha1.LegacyTargetable{},
	}, {
		name: "targetable",
		t:    &duckv1alpha1.Targetable{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Route{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Route, %T) = %v", test.t, err)
			}
		})
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
			Conditions: duckv1alpha1.Conditions{{
				Type:   RouteConditionAllTrafficAssigned,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: RouteStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: RouteStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: RouteStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type: RouteConditionReady,
			}},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: RouteStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   RouteConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: RouteStatus{
			Conditions: duckv1alpha1.Conditions{{
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
			Conditions: duckv1alpha1.Conditions{{
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

func TestTypicalRouteFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkTrafficAssigned()
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.PropagateClusterIngressStatus(netv1alpha1.IngressStatus{
		Conditions: duckv1alpha1.Conditions{{
			Type:   netv1alpha1.ClusterIngressConditionReady,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)

	// Verify that this doesn't reset our conditions.
	r.Status.InitializeConditions()
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)
}

func TestTrafficNotAssignedFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkMissingTrafficTarget("Revision", "does-not-exist")
	checkConditionFailedRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionFailedRoute(r.Status, RouteConditionReady, t)
}

func TestTargetConfigurationNotYetReadyFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkConfigurationNotReady("i-have-no-ready-revision")
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)
}

func TestUnknownErrorWhenConfiguringTraffic(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkUnknownTrafficError("unknown-error")
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)
}

func TestTargetConfigurationFailedToBeReadyFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkConfigurationFailed("permanently-failed")
	checkConditionFailedRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionFailedRoute(r.Status, RouteConditionReady, t)
}

func TestTargetRevisionNotYetReadyFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkRevisionNotReady("not-yet-ready")
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)
}

func TestTargetRevisionFailedToBeReadyFlow(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkRevisionFailed("cannot-find-image")
	checkConditionFailedRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionFailedRoute(r.Status, RouteConditionReady, t)
}

func TestClusterIngressFailureRecovery(t *testing.T) {
	r := &Route{}
	r.Status.InitializeConditions()
	r.Status.PropagateClusterIngressStatus(netv1alpha1.IngressStatus{
		Conditions: duckv1alpha1.Conditions{{
			Type:   netv1alpha1.ClusterIngressConditionReady,
			Status: corev1.ConditionUnknown,
		}},
	})
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	// Empty IngressStatus keeps things as-is.
	r.Status.PropagateClusterIngressStatus(netv1alpha1.IngressStatus{})
	checkConditionOngoingRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionOngoingRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionOngoingRoute(r.Status, RouteConditionReady, t)

	r.Status.MarkTrafficAssigned()
	r.Status.PropagateClusterIngressStatus(netv1alpha1.IngressStatus{
		Conditions: duckv1alpha1.Conditions{{
			Type:   netv1alpha1.ClusterIngressConditionReady,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)

	r.Status.PropagateClusterIngressStatus(netv1alpha1.IngressStatus{
		Conditions: duckv1alpha1.Conditions{{
			Type:   netv1alpha1.ClusterIngressConditionReady,
			Status: corev1.ConditionFalse,
		}},
	})
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionFailedRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionFailedRoute(r.Status, RouteConditionReady, t)

	r.Status.PropagateClusterIngressStatus(netv1alpha1.IngressStatus{
		Conditions: duckv1alpha1.Conditions{{
			Type:   netv1alpha1.ClusterIngressConditionReady,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionSucceededRoute(r.Status, RouteConditionAllTrafficAssigned, t)
	checkConditionSucceededRoute(r.Status, RouteConditionIngressReady, t)
	checkConditionSucceededRoute(r.Status, RouteConditionReady, t)
}

func checkConditionSucceededRoute(rs RouteStatus, rct duckv1alpha1.ConditionType, t *testing.T) {
	t.Helper()
	checkConditionRoute(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedRoute(rs RouteStatus, rct duckv1alpha1.ConditionType, t *testing.T) {
	t.Helper()
	checkConditionRoute(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingRoute(rs RouteStatus, rct duckv1alpha1.ConditionType, t *testing.T) {
	t.Helper()
	checkConditionRoute(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionRoute(rs RouteStatus, rct duckv1alpha1.ConditionType, cs corev1.ConditionStatus, t *testing.T) {
	t.Helper()
	r := rs.GetCondition(rct)
	if r == nil {
		t.Fatalf("Get(%v) = nil, wanted %v=%v", rct, rct, cs)
	}
	if r.Status != cs {
		t.Fatalf("Get(%v) = %v, wanted %v", rct, r.Status, cs)
	}
}

func TestRouteGetGroupVersionKind(t *testing.T) {
	r := &Route{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1alpha1",
		Kind:    "Route",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
