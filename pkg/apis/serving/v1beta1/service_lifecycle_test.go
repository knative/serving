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
package v1beta1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestServiceDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
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

func TestServiceGetGroupVersionKind(t *testing.T) {
	r := &Service{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1beta1",
		Kind:    "Service",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestServiceIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  v1.ServiceStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  v1.ServiceStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: v1.ServiceStatus{
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
		status: v1.ServiceStatus{
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
		status: v1.ServiceStatus{
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
		status: v1.ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: ServiceConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: v1.ServiceStatus{
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
		status: v1.ServiceStatus{
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
		status: v1.ServiceStatus{
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

func TestServiceIsFailed(t *testing.T) {
	cases := []struct {
		name     string
		status   v1.ServiceStatus
		isFailed bool
	}{{
		name:     "empty status should not be failed",
		status:   v1.ServiceStatus{},
		isFailed: false,
	}, {
		name: "False condition status should be failed",
		status: v1.ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isFailed: true,
	}, {
		name: "Unknown condition status should not be failed",
		status: v1.ServiceStatus{
			Status: duckv1.Status{

				Conditions: duckv1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isFailed: false,
	}, {
		name: "Missing condition status should not be failed",
		status: v1.ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: ServiceConditionReady,
				}},
			},
		},
		isFailed: false,
	}, {
		name: "True condition status should not be failed",
		status: v1.ServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ServiceConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isFailed: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Service{Status: tc.status}
			if e, a := tc.isFailed, r.IsFailed(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}
		})
	}
}
