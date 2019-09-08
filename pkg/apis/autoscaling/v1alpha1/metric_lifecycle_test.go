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
package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apitestv1 "knative.dev/pkg/apis/testing/v1"
	"knative.dev/serving/pkg/apis/autoscaling"
)

func TestMetricDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Metric{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Metric, %T) = %v", test.t, err)
			}
		})
	}
}

func TestMetricIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  MetricStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  MetricStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: MetricStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   "FooCondition",
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: MetricStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   MetricConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: MetricStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   MetricConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: MetricStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: MetricConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: MetricStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   MetricConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e, a := tc.isReady, tc.status.IsReady(); e != a {
				t.Errorf("Ready = %v, want: %v", a, e)
			}
		})
	}
}

func TestMetricGetSetCondition(t *testing.T) {
	ms := &MetricStatus{}
	if a := ms.GetCondition(MetricConditionReady); a != nil {
		t.Errorf("empty MetricStatus returned %v when expected nil", a)
	}
	mc := &apis.Condition{
		Type:   MetricConditionReady,
		Status: corev1.ConditionTrue,
	}
	ms.MarkMetricReady()
	if diff := cmp.Diff(mc, ms.GetCondition(MetricConditionReady), cmpopts.IgnoreFields(apis.Condition{}, "LastTransitionTime")); diff != "" {
		t.Errorf("GetCondition refs diff (-want +got): %v", diff)
	}
}

func TestTypicalFlowWithMetricCondition(t *testing.T) {
	m := &MetricStatus{}
	m.InitializeConditions()
	apitestv1.CheckConditionOngoing(m.duck(), MetricConditionReady, t)

	const (
		wantReason  = "reason"
		wantMessage = "the error message"
	)
	m.MarkMetricFailed(wantReason, wantMessage)
	apitestv1.CheckConditionFailed(m.duck(), MetricConditionReady, t)
	if got := m.GetCondition(MetricConditionReady); got == nil || got.Reason != wantReason || got.Message != wantMessage {
		t.Errorf("MarkMetricFailed = %v, wantReason %v, wantMessage %v", got, wantReason, wantMessage)
	}

	m.MarkMetricNotReady(wantReason, wantMessage)
	apitestv1.CheckConditionOngoing(m.duck(), MetricConditionReady, t)
	if got := m.GetCondition(MetricConditionReady); got == nil || got.Reason != wantReason || got.Message != wantMessage {
		t.Errorf("MarkMetricNotReady = %v, wantReason %v, wantMessage %v", got, wantReason, wantMessage)
	}

	m.MarkMetricReady()
	apitestv1.CheckConditionSucceeded(m.duck(), MetricConditionReady, t)
}

func TestMetricGetGroupVersionKind(t *testing.T) {
	r := &Metric{}
	want := schema.GroupVersionKind{
		Group:   autoscaling.InternalGroupName,
		Version: "v1alpha1",
		Kind:    "Metric",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
