/*
Copyright 2019 The Knative Authors.

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
package serving

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestTransformDeploymentStatus(t *testing.T) {
	tests := []struct {
		name string
		ds   *appsv1.DeploymentStatus
		want *duckv1.Status
	}{{
		name: "initial conditions",
		ds:   &appsv1.DeploymentStatus{},
		want: &duckv1.Status{
			Conditions: []apis.Condition{{
				Type:   DeploymentConditionProgressing,
				Status: corev1.ConditionUnknown,
			}, {
				Type:   DeploymentConditionReplicaSetReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   DeploymentConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	}, {
		name: "happy without rs",
		ds: &appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionTrue,
			}},
		},
		want: &duckv1.Status{
			Conditions: []apis.Condition{{
				Type:   DeploymentConditionProgressing,
				Status: corev1.ConditionTrue,
			}, {
				Type:   DeploymentConditionReplicaSetReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   DeploymentConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}, {
		name: "happy with rs",
		ds: &appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionTrue,
			}, {
				Type:   appsv1.DeploymentReplicaFailure,
				Status: corev1.ConditionFalse,
			}},
		},
		want: &duckv1.Status{
			Conditions: []apis.Condition{{
				Type:   DeploymentConditionProgressing,
				Status: corev1.ConditionTrue,
			}, {
				Type:   DeploymentConditionReplicaSetReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   DeploymentConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}, {
		name: "false progressing unknown rs",
		ds: &appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionFalse,
			}, {
				Type:   appsv1.DeploymentReplicaFailure,
				Status: corev1.ConditionUnknown,
			}},
		},
		want: &duckv1.Status{
			Conditions: []apis.Condition{{
				Type:   DeploymentConditionProgressing,
				Status: corev1.ConditionFalse,
			}, {
				Type:   DeploymentConditionReplicaSetReady,
				Status: corev1.ConditionUnknown,
			}, {
				Type:   DeploymentConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}, {
		name: "unknown progressing failed rs",
		ds: &appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionUnknown,
			}, {
				Type:   appsv1.DeploymentReplicaFailure,
				Status: corev1.ConditionTrue,
			}},
		},
		want: &duckv1.Status{
			Conditions: []apis.Condition{{
				Type:   DeploymentConditionProgressing,
				Status: corev1.ConditionUnknown,
			}, {
				Type:   DeploymentConditionReplicaSetReady,
				Status: corev1.ConditionFalse,
			}, {
				Type:   DeploymentConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}, {
		name: "reason and message propagate",
		ds: &appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionUnknown,
			}, {
				Type:    appsv1.DeploymentReplicaFailure,
				Status:  corev1.ConditionTrue,
				Reason:  "ReplicaSetReason",
				Message: "Something bag happened",
			}},
		},
		want: &duckv1.Status{
			Conditions: []apis.Condition{{
				Type:   DeploymentConditionProgressing,
				Status: corev1.ConditionUnknown,
			}, {
				Type:    DeploymentConditionReplicaSetReady,
				Status:  corev1.ConditionFalse,
				Reason:  "ReplicaSetReason",
				Message: "Something bag happened",
			}, {
				Type:    DeploymentConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  "ReplicaSetReason",
				Message: "Something bag happened",
			}},
		},
	}}

	opts := []cmp.Option{
		cmpopts.IgnoreFields(apis.Condition{}, "LastTransitionTime"),
		cmpopts.SortSlices(func(a, b apis.Condition) bool {
			return a.Type < b.Type
		}),
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want, got := test.want, TransformDeploymentStatus(test.ds)
			if diff := cmp.Diff(want, got, opts...); diff != "" {
				t.Errorf("GetCondition refs diff (-want +got): %v", diff)
			}
		})
	}
}
