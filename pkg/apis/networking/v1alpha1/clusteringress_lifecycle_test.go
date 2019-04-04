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
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestClusterIngressDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1alpha1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&ClusterIngress{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(ClusterIngress, %T) = %v", test.t, err)
			}
		})
	}
}

func TestCIGetGroupVersionKind(t *testing.T) {
	ci := ClusterIngress{}
	expected := SchemeGroupVersion.WithKind("ClusterIngress")
	if diff := cmp.Diff(expected, ci.GetGroupVersionKind()); diff != "" {
		t.Errorf("Unexpected diff (-want, +got) = %v", diff)
	}
}

func TestIsPublic(t *testing.T) {
	ci := ClusterIngress{}
	if !ci.IsPublic() {
		t.Error("Expected default ClusterIngress to be public, for backward compatibility")
	}
	if !ci.IsPublic() {
		t.Errorf("Expected IsPublic()==true, saw %v", ci.IsPublic())
	}
	ci.Spec.Visibility = IngressVisibilityExternalIP
	if !ci.IsPublic() {
		t.Errorf("Expected IsPublic()==true, saw %v", ci.IsPublic())
	}
	ci.Spec.Visibility = IngressVisibilityClusterLocal
	if ci.IsPublic() {
		t.Errorf("Expected IsPublic()==false, saw %v", ci.IsPublic())
	}
}

func TestCITypicalFlow(t *testing.T) {
	r := &IngressStatus{}
	r.InitializeConditions()

	checkConditionOngoingClusterIngress(r, ClusterIngressConditionReady, t)

	// Then network is configured.
	r.MarkNetworkConfigured()
	checkConditionSucceededClusterIngress(r, ClusterIngressConditionNetworkConfigured, t)
	checkConditionOngoingClusterIngress(r, ClusterIngressConditionReady, t)

	// Then ingress has address.
	r.MarkLoadBalancerReady([]LoadBalancerIngressStatus{{DomainInternal: "gateway.default.svc"}})
	checkConditionSucceededClusterIngress(r, ClusterIngressConditionLoadBalancerReady, t)
	checkConditionSucceededClusterIngress(r, ClusterIngressConditionReady, t)
	checkIsReady(r, t)
	// Mark not owned.
	r.MarkResourceNotOwned("i own", "you")
	checkConditionFailedClusterIngress(r, ClusterIngressConditionReady, t)
}

// TODO(vagababov): move this outside and re-use elsewhere.
type ConditionCheckable interface {
	IsReady() bool
	GetCondition(t apis.ConditionType) *apis.Condition
}

func checkIsReady(cc ConditionCheckable, t *testing.T) {
	t.Helper()
	if !cc.IsReady() {
		t.Fatal("IsReady()=false, wanted true")
	}
}

func checkConditionSucceededClusterIngress(cc ConditionCheckable, c apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkCondition(cc, c, corev1.ConditionTrue, t)
}

func checkConditionFailedClusterIngress(cc ConditionCheckable, c apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkCondition(cc, c, corev1.ConditionFalse, t)
}

func checkConditionOngoingClusterIngress(cc ConditionCheckable, c apis.ConditionType, t *testing.T) *apis.Condition {
	t.Helper()
	return checkCondition(cc, c, corev1.ConditionUnknown, t)
}

func checkCondition(cc ConditionCheckable, c apis.ConditionType, cs corev1.ConditionStatus, t *testing.T) *apis.Condition {
	t.Helper()
	cond := cc.GetCondition(c)
	if cond == nil {
		t.Fatalf("Get(%v) = nil, wanted %v=%v", c, c, cs)
	}
	if cond.Status != cs {
		t.Fatalf("Get(%v) = %v, wanted %v", c, cond.Status, cs)
	}
	return cond
}
