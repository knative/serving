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

func TestCertificateDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Certificate{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Certificate, %T) = %v", test.t, err)
			}
		})
	}
}

func TestCertificateGetGroupVersionKind(t *testing.T) {
	c := Certificate{}
	expected := SchemeGroupVersion.WithKind("Certificate")
	if diff := cmp.Diff(expected, c.GetGroupVersionKind()); diff != "" {
		t.Errorf("Unexpected diff (-want, +got) = %s", diff)
	}
}

func TestMarkReady(t *testing.T) {
	c := &CertificateStatus{}
	c.InitializeConditions()
	apitestv1.CheckConditionOngoing(c.duck(), CertificateConditionReady, t)

	c.MarkReady()
	if !c.IsReady() {
		t.Error("IsReady=false, want: true")
	}
}

func TestMarkNotReady(t *testing.T) {
	c := &CertificateStatus{}
	c.InitializeConditions()
	apitestv1.CheckCondition(c.duck(), CertificateConditionReady, corev1.ConditionUnknown)

	c.MarkNotReady("unknow", "unknown")
	apitestv1.CheckCondition(c.duck(), CertificateConditionReady, corev1.ConditionUnknown)
}

func TestMarkFailed(t *testing.T) {
	c := &CertificateStatus{}
	c.InitializeConditions()
	apitestv1.CheckCondition(c.duck(), CertificateConditionReady, corev1.ConditionUnknown)

	c.MarkFailed("failed", "failed")
	apitestv1.CheckConditionFailed(c.duck(), CertificateConditionReady, t)
}

func TestMarkResourceNotOwned(t *testing.T) {
	c := &CertificateStatus{}
	c.InitializeConditions()
	c.MarkResourceNotOwned("doesn't", "own")
	apitestv1.CheckConditionFailed(c.duck(), CertificateConditionReady, t)
}

func TestGetCondition(t *testing.T) {
	c := &CertificateStatus{}
	c.InitializeConditions()
	tests := []struct {
		name     string
		condType apis.ConditionType
		expect   *apis.Condition
		reason   string
		message  string
	}{{
		name:     "random condition",
		condType: apis.ConditionType("random"),
		expect:   nil,
	}, {
		name:     "ready condition for failed reason",
		condType: apis.ConditionReady,
		reason:   "failed",
		message:  "failed",
		expect: &apis.Condition{
			Status: corev1.ConditionFalse,
		},
	}, {
		name:     "ready condition for unknown reason",
		condType: apis.ConditionReady,
		reason:   "unknown",
		message:  "unknown",
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
			if tc.reason == "unknown" {
				c.MarkNotReady(tc.reason, tc.message)
			} else {
				c.MarkFailed(tc.reason, tc.message)
			}
			if got, want := c.GetCondition(tc.condType), tc.expect; got != nil && got.Status != want.Status {
				t.Errorf("got: %v, want: %v", got, want)
			}
		})
	}
}
