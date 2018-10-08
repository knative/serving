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
	"time"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodAutoscalerDuckTypes(t *testing.T) {
	var emptyGen duckv1alpha1.Generation

	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "generations",
		t:    &emptyGen,
	}, {
		name: "conditions",
		t:    &duckv1alpha1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&PodAutoscaler{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(PodAutoscaler, %T) = %v", test.t, err)
			}
		})
	}
}

func TestGeneration(t *testing.T) {
	r := PodAutoscaler{}
	if a := r.GetGeneration(); a != 0 {
		t.Errorf("empty kpa generation should be 0 was: %d", a)
	}

	r.SetGeneration(5)
	if e, a := int64(5), r.GetGeneration(); e != a {
		t.Errorf("getgeneration mismatch expected: %d got: %d", e, a)
	}

}

func TestCanScaleToZero(t *testing.T) {
	cases := []struct {
		name   string
		status PodAutoscalerStatus
		result bool
		grace  time.Duration
	}{{
		name:   "empty status",
		status: PodAutoscalerStatus{},
		result: false,
		grace:  10 * time.Second,
	}, {
		name: "active condition",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionTrue,
			}},
		},
		result: false,
		grace:  10 * time.Second,
	}, {
		name: "inactive condition (no LTT)",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionFalse,
				// No LTT = beginning of time, so for sure we can.
			}},
		},
		result: true,
		grace:  10 * time.Second,
	}, {
		name: "inactive condition (LTT longer than grace period ago)",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionFalse,
				LastTransitionTime: apis.VolatileTime{
					Inner: metav1.NewTime(time.Now().Add(-30 * time.Second)),
				},
				// LTT = 30 seconds ago.
			}},
		},
		result: true,
		grace:  10 * time.Second,
	}, {
		name: "inactive condition (LTT less than grace period ago)",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionFalse,
				LastTransitionTime: apis.VolatileTime{
					Inner: metav1.NewTime(time.Now().Add(-10 * time.Second)),
				},
				// LTT = 10 seconds ago.
			}},
		},
		result: false,
		grace:  30 * time.Second,
	}}

	for _, tc := range cases {
		if e, a := tc.result, tc.status.CanScaleToZero(tc.grace); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  PodAutoscalerStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  PodAutoscalerStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type: PodAutoscalerConditionReady,
			}},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionTrue,
			}, {
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: PodAutoscalerStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionTrue,
			}, {
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}}

	for _, tc := range cases {
		if e, a := tc.isReady, tc.status.IsReady(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestTypicalFlow(t *testing.T) {
	r := &PodAutoscaler{}
	r.Status.InitializeConditions()
	checkConditionOngoingPodAutoscaler(r.Status, PodAutoscalerConditionActive, t)
	checkConditionOngoingPodAutoscaler(r.Status, PodAutoscalerConditionReady, t)

	// When we see traffic, mark ourselves active.
	r.Status.MarkActive()
	checkConditionSucceededPodAutoscaler(r.Status, PodAutoscalerConditionActive, t)
	checkConditionSucceededPodAutoscaler(r.Status, PodAutoscalerConditionReady, t)

	// Check idempotency.
	r.Status.MarkActive()
	checkConditionSucceededPodAutoscaler(r.Status, PodAutoscalerConditionActive, t)
	checkConditionSucceededPodAutoscaler(r.Status, PodAutoscalerConditionReady, t)

	// When we stop seeing traffic, mark outselves inactive.
	r.Status.MarkInactive("TheReason", "the message")
	checkConditionFailedPodAutoscaler(r.Status, PodAutoscalerConditionActive, t)
	checkConditionFailedPodAutoscaler(r.Status, PodAutoscalerConditionReady, t)

	// When traffic hits the activator and we scale up the deployment we mark
	// ourselves as activating.
	r.Status.MarkActivating("Activating", "Red team, GO!")
	checkConditionOngoingPodAutoscaler(r.Status, PodAutoscalerConditionActive, t)
	checkConditionOngoingPodAutoscaler(r.Status, PodAutoscalerConditionReady, t)

	// When the activator successfully forwards traffic to the deployment,
	// we mark ourselves as active once more.
	r.Status.MarkActive()
	checkConditionSucceededPodAutoscaler(r.Status, PodAutoscalerConditionActive, t)
	checkConditionSucceededPodAutoscaler(r.Status, PodAutoscalerConditionReady, t)
}

func checkConditionSucceededPodAutoscaler(rs PodAutoscalerStatus, rct duckv1alpha1.ConditionType, t *testing.T) *duckv1alpha1.Condition {
	t.Helper()
	return checkConditionPodAutoscaler(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedPodAutoscaler(rs PodAutoscalerStatus, rct duckv1alpha1.ConditionType, t *testing.T) *duckv1alpha1.Condition {
	t.Helper()
	return checkConditionPodAutoscaler(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingPodAutoscaler(rs PodAutoscalerStatus, rct duckv1alpha1.ConditionType, t *testing.T) *duckv1alpha1.Condition {
	t.Helper()
	return checkConditionPodAutoscaler(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionPodAutoscaler(rs PodAutoscalerStatus, rct duckv1alpha1.ConditionType, cs corev1.ConditionStatus, t *testing.T) *duckv1alpha1.Condition {
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
