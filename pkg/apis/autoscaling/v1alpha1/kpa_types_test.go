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

	sapis "github.com/knative/serving/pkg/apis"
	corev1 "k8s.io/api/core/v1"
)

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
			Conditions: sapis.Conditions{{
				Type:   PodAutoscalerConditionActive,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: PodAutoscalerStatus{
			Conditions: sapis.Conditions{{
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: PodAutoscalerStatus{
			Conditions: sapis.Conditions{{
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: PodAutoscalerStatus{
			Conditions: sapis.Conditions{{
				Type: PodAutoscalerConditionReady,
			}},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: PodAutoscalerStatus{
			Conditions: sapis.Conditions{{
				Type:   PodAutoscalerConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: PodAutoscalerStatus{
			Conditions: sapis.Conditions{{
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
			Conditions: sapis.Conditions{{
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

func checkConditionSucceededPodAutoscaler(rs PodAutoscalerStatus, rct sapis.ConditionType, t *testing.T) *sapis.Condition {
	t.Helper()
	return checkConditionPodAutoscaler(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedPodAutoscaler(rs PodAutoscalerStatus, rct sapis.ConditionType, t *testing.T) *sapis.Condition {
	t.Helper()
	return checkConditionPodAutoscaler(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingPodAutoscaler(rs PodAutoscalerStatus, rct sapis.ConditionType, t *testing.T) *sapis.Condition {
	t.Helper()
	return checkConditionPodAutoscaler(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionPodAutoscaler(rs PodAutoscalerStatus, rct sapis.ConditionType, cs corev1.ConditionStatus, t *testing.T) *sapis.Condition {
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
