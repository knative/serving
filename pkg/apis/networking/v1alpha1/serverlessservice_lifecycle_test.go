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
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apistest "knative.dev/pkg/apis/testing"
)

func TestServerlessServiceDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
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

func TestServerlessServiceGetConditionSet(t *testing.T) {
	r := &ServerlessService{}
	if got, want := r.GetConditionSet().GetTopLevelConditionType(), apis.ConditionReady; got != want {
		t.Errorf("GetConditionSet=%v, want=%v", got, want)
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

	apistest.CheckConditionOngoing(r, ServerlessServiceConditionReady, t)

	r.MarkEndpointsReady()
	apistest.CheckConditionSucceeded(r, ServerlessServiceConditionEndspointsPopulated, t)
	apistest.CheckConditionSucceeded(r, ServerlessServiceConditionReady, t)

	// Verify that activator endpoints status is informational and does not
	// affect readiness.
	r.MarkActivatorEndpointsPopulated()
	apistest.CheckConditionSucceeded(r, ServerlessServiceConditionReady, t)
	r.MarkActivatorEndpointsRemoved()
	apistest.CheckConditionSucceeded(r, ServerlessServiceConditionReady, t)

	// Or another way to check the same condition.
	ss := &ServerlessService{Status: *r}
	if !ss.IsReady() {
		t.Error("IsReady=false, want: true")
	}
	r.MarkEndpointsNotReady("random")
	apistest.CheckConditionOngoing(r, ServerlessServiceConditionReady, t)

	// Verify that activator endpoints status is informational and does not
	// affect readiness.
	r.MarkActivatorEndpointsPopulated()
	apistest.CheckConditionOngoing(r, ServerlessServiceConditionReady, t)
	r.MarkActivatorEndpointsRemoved()
	apistest.CheckConditionOngoing(r, ServerlessServiceConditionReady, t)

	r.MarkEndpointsNotOwned("service", "jukebox")
	apistest.CheckConditionFailed(r, ServerlessServiceConditionReady, t)

	// Verify that activator endpoints status is informational and does not
	// affect readiness.
	r.MarkActivatorEndpointsPopulated()
	apistest.CheckConditionFailed(r, ServerlessServiceConditionReady, t)
	apistest.CheckConditionSucceeded(r, ActivatorEndpointsPopulated, t)

	time.Sleep(time.Millisecond * 1)
	if got, want := r.ProxyFor(), time.Duration(0); got == want {
		t.Error("ProxyFor returned duration of 0")
	}

	r.MarkActivatorEndpointsRemoved()
	apistest.CheckConditionFailed(r, ServerlessServiceConditionReady, t)
	apistest.CheckConditionFailed(r, ActivatorEndpointsPopulated, t)

	if got, want := r.ProxyFor(), time.Duration(0); got != want {
		t.Errorf("ProxyFor = %v, want: %v", got, want)
	}
}
