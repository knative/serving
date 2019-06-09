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
	"github.com/knative/pkg/apis/duck"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1alpha1"
	apitest "github.com/knative/pkg/apis/testing"
)

func TestIngressDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1beta1.Conditions{},
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

	apitest.CheckConditionOngoing(r.duck(), IngressConditionReady, t)

	// Then network is configured.
	r.MarkNetworkConfigured()
	apitest.CheckConditionSucceeded(r.duck(), IngressConditionNetworkConfigured, t)
	apitest.CheckConditionOngoing(r.duck(), IngressConditionReady, t)

	// Then ingress has address.
	r.MarkLoadBalancerReady([]LoadBalancerIngressStatus{{DomainInternal: "gateway.default.svc"}})
	apitest.CheckConditionSucceeded(r.duck(), IngressConditionLoadBalancerReady, t)
	apitest.CheckConditionSucceeded(r.duck(), IngressConditionReady, t)
	if !r.IsReady() {
		t.Fatal("IsReady()=false, wanted true")
	}

	// Mark not owned.
	r.MarkResourceNotOwned("i own", "you")
	apitest.CheckConditionFailed(r.duck(), IngressConditionReady, t)
}
