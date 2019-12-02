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
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apitestv1 "knative.dev/pkg/apis/testing/v1"
)

func TestIngressDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Ingress{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Ingress, %T) = %v", test.t, err)
			}
		})
	}
}

func TestIngressGetGroupVersionKind(t *testing.T) {
	ci := Ingress{}
	expected := SchemeGroupVersion.WithKind("Ingress")
	if diff := cmp.Diff(expected, ci.GetGroupVersionKind()); diff != "" {
		t.Errorf("Unexpected diff (-want, +got) = %v", diff)
	}
}

func TestIngressIsPublic(t *testing.T) {
	ci := Ingress{}
	if !ci.IsPublic() {
		t.Error("Expected default Ingress to be public, for backward compatibility")
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

func TestIngressTypicalFlow(t *testing.T) {
	r := &IngressStatus{}
	r.InitializeConditions()

	apitestv1.CheckConditionOngoing(r.duck(), IngressConditionReady, t)

	// Then network is configured.
	r.MarkNetworkConfigured()
	apitestv1.CheckConditionSucceeded(r.duck(), IngressConditionNetworkConfigured, t)
	apitestv1.CheckConditionOngoing(r.duck(), IngressConditionReady, t)

	// Then ingress is pending.
	r.MarkLoadBalancerNotReady()
	apitestv1.CheckConditionOngoing(r.duck(), IngressConditionLoadBalancerReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), IngressConditionReady, t)

	r.MarkLoadBalancerFailed("some reason", "some message")
	apitestv1.CheckConditionFailed(r.duck(), IngressConditionLoadBalancerReady, t)
	apitestv1.CheckConditionFailed(r.duck(), IngressConditionLoadBalancerReady, t)

	// Then ingress has address.
	r.MarkLoadBalancerReady(
		[]LoadBalancerIngressStatus{{DomainInternal: "gateway.default.svc"}},
		[]LoadBalancerIngressStatus{{DomainInternal: "gateway.default.svc"}},
		[]LoadBalancerIngressStatus{{DomainInternal: "private.gateway.default.svc"}},
	)
	apitestv1.CheckConditionSucceeded(r.duck(), IngressConditionLoadBalancerReady, t)
	apitestv1.CheckConditionSucceeded(r.duck(), IngressConditionReady, t)
	if !r.IsReady() {
		t.Fatal("IsReady()=false, wanted true")
	}

	// Mark not owned.
	r.MarkResourceNotOwned("i own", "you")
	apitestv1.CheckConditionFailed(r.duck(), IngressConditionReady, t)

	// Mark network configured, and check that ingress is ready again
	r.MarkNetworkConfigured()
	apitestv1.CheckConditionSucceeded(r.duck(), IngressConditionReady, t)
	if !r.IsReady() {
		t.Fatal("IsReady()=false, wanted true")
	}

	// Mark ingress not ready
	r.MarkIngressNotReady("", "")
	apitestv1.CheckConditionOngoing(r.duck(), IngressConditionReady, t)
}

func TestIngressGetCondition(t *testing.T) {
	ingressStatus := &IngressStatus{}
	ingressStatus.InitializeConditions()
	tests := []struct {
		name     string
		condType apis.ConditionType
		expect   *apis.Condition
	}{{
		name:     "random condition",
		condType: apis.ConditionType("random"),
		expect:   nil,
	}, {
		name:     "ready condition",
		condType: apis.ConditionReady,
		expect: &apis.Condition{
			Status: corev1.ConditionUnknown,
		},
	}, {
		name:     "succeeded condition",
		condType: apis.ConditionSucceeded,
		expect:   nil,
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := ingressStatus.GetCondition(tc.condType), tc.expect; got != nil && got.Status != want.Status {
				t.Errorf("got: %v, want: %v", got, want)
			}
		})
	}
}
