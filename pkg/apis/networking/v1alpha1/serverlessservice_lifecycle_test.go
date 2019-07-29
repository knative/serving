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
	"time"

	"github.com/google/go-cmp/cmp"
	"knative.dev/pkg/apis/duck"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	apitest "knative.dev/pkg/apis/testing"
)

func TestServerlessServiceDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1beta1.Conditions{},
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

	apitest.CheckConditionOngoing(r.duck(), ServerlessServiceConditionReady, t)

	r.MarkEndpointsReady()
	apitest.CheckConditionSucceeded(r.duck(), ServerlessServiceConditionEndspointsPopulated, t)
	apitest.CheckConditionSucceeded(r.duck(), ServerlessServiceConditionReady, t)

	// Verify that activator endpoints status is informational and does not
	// affect readiness.
	r.MarkActivatorEndpointsPopulated()
	apitest.CheckConditionSucceeded(r.duck(), ServerlessServiceConditionReady, t)
	r.MarkActivatorEndpointsRemoved()
	apitest.CheckConditionSucceeded(r.duck(), ServerlessServiceConditionReady, t)

	// Or another way to check the same condition.
	if !r.IsReady() {
		t.Error("IsReady=false, want: true")
	}
	r.MarkEndpointsNotReady("random")
	apitest.CheckConditionOngoing(r.duck(), ServerlessServiceConditionReady, t)

	// Verify that activator endpoints status is informational and does not
	// affect readiness.
	r.MarkActivatorEndpointsPopulated()
	apitest.CheckConditionOngoing(r.duck(), ServerlessServiceConditionReady, t)
	r.MarkActivatorEndpointsRemoved()
	apitest.CheckConditionOngoing(r.duck(), ServerlessServiceConditionReady, t)

	r.MarkEndpointsNotOwned("service", "jukebox")
	apitest.CheckConditionFailed(r.duck(), ServerlessServiceConditionReady, t)

	// Verify that activator endpoints status is informational and does not
	// affect readiness.
	r.MarkActivatorEndpointsPopulated()
	apitest.CheckConditionFailed(r.duck(), ServerlessServiceConditionReady, t)
	apitest.CheckConditionSucceeded(r.duck(), ActivatorEndpointsPopulated, t)

	time.Sleep(time.Millisecond * 1)
	if got, want := r.ProxyFor(), time.Duration(0); got == want {
		t.Error("ProxyFor returned duration of 0")
	}

	r.MarkActivatorEndpointsRemoved()
	apitest.CheckConditionFailed(r.duck(), ServerlessServiceConditionReady, t)
	apitest.CheckConditionFailed(r.duck(), ActivatorEndpointsPopulated, t)

	if got, want := r.ProxyFor(), time.Duration(0); got != want {
		t.Errorf("ProxyFor = %v, want: %v", got, want)
	}
}
