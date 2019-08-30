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
package v1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestRouteDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
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

func TestRouteGetGroupVersionKind(t *testing.T) {
	r := &Route{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1",
		Kind:    "Route",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestRouteIsReady(t *testing.T) {
	tests := []struct {
		name     string
		rs       *RouteStatus
		expected bool
	}{{
		name:     "Ready undefined",
		rs:       &RouteStatus{},
		expected: false,
	}, {
		name: "Ready=False",
		rs: &RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		expected: false,
	}, {
		name: "Ready=Unknown",
		rs: &RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		expected: false,
	}, {
		name: "Ready=True",
		rs: &RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		expected: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ready := test.rs.IsReady()
			if ready != test.expected {
				t.Errorf("IsReady() = %t; expected %t", ready, test.expected)
			}
		})
	}
}
