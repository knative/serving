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
package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestServerlessServiceDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1alpha1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&ServerlessService{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(ServerlessService, %T) = %v", test.t, err)
			}
		})
	}
}

func TestGetGroupVersionKind(t *testing.T) {
	ss := ServerlessService{}
	expected := SchemeGroupVersion.WithKind("ServerlessService")
	if diff := cmp.Diff(expected, ss.GetGroupVersionKind()); diff != "" {
		t.Errorf("Unexpected diff (-want, +got) = %v", diff)
	}
}

func TestSSTypicalFlow(t *testing.T) {
	r := &ServerlessServiceStatus{}
	r.InitializeConditions()

	checkConditionOngoingClusterIngress(r, ServerlessServiceConditionReady, t)

	r.MarkEndpointsPopulated()
	checkConditionSucceededServerlessService(r, ServerlessServiceConditionEndspointsPopulated, t)
	checkConditionSucceededServerlessService(r, ServerlessServiceConditionReady, t)
	checkIsReady(r, t)
}

func checkConditionSucceededServerlessService(cc ConditionCheckable, c apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkCondition(cc, c, corev1.ConditionTrue, t)
}

func checkConditionOngoingServerlessService(cc ConditionCheckable, c apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkCondition(cc, c, corev1.ConditionUnknown, t)
}
