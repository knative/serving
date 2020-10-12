/*
Copyright 2020 The Knative Authors

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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apistest "knative.dev/pkg/apis/testing"
)

func TestDomainMappingDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&DomainMapping{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(DomainMapping, %T) = %v", test.t, err)
			}
		})
	}
}

func TestDomainMappingGetConditionSet(t *testing.T) {
	r := &DomainMapping{}

	if got, want := r.GetConditionSet().GetTopLevelConditionType(), apis.ConditionReady; got != want {
		t.Errorf("GetTopLevelCondition=%v, want=%v", got, want)
	}
}

func TestDomainMappingGetGroupVersionKind(t *testing.T) {
	r := &DomainMapping{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1alpha1",
		Kind:    "DomainMapping",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestPropagateIngressStatus(t *testing.T) {
	dms := &DomainMappingStatus{}

	dms.InitializeConditions()
	apistest.CheckConditionOngoing(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)

	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})

	apistest.CheckConditionFailed(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionFailed(dms, DomainMappingConditionReady, t)

	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})

	apistest.CheckConditionSucceeded(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionSucceeded(dms, DomainMappingConditionReady, t)

	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})

	apistest.CheckConditionOngoing(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)
}
