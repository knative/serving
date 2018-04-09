/*
Copyright 2018 Google LLC
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

package service

import (
	"testing"

	"github.com/elafros/elafros/pkg/apis/ela"
)

const (
	testConfigName string = "test-configuration"
)

func TestRouteRunLatest(t *testing.T) {
	s := createServiceWithRunLatest()
	r := MakeServiceRoute(s, testConfigName)
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("expected %q for service namespace got %q", want, got)
	}
	if got, want := len(r.Spec.Traffic), 1; got != want {
		t.Fatalf("expected %d traffic targets got %d", want, got)
	}
	tt := r.Spec.Traffic[0]
	if got, want := tt.Percent, 100; got != want {
		t.Errorf("expected %d percent got %d", want, got)
	}
	if got, want := tt.RevisionName, ""; got != want {
		t.Errorf("expected %q revisionName got %q", want, got)
	}
	if got, want := tt.ConfigurationName, testConfigName; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValueRunLatest; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[ela.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func TestRoutePinned(t *testing.T) {
	s := createServiceWithPinned()
	r := MakeServiceRoute(s, testConfigName)
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("expected %q for service namespace got %q", want, got)
	}
	if got, want := len(r.Spec.Traffic), 1; got != want {
		t.Fatalf("expected %d traffic targets, got %d", want, got)
	}
	tt := r.Spec.Traffic[0]
	if got, want := tt.Percent, 100; got != want {
		t.Errorf("expected %d percent got %d", want, got)
	}
	if got, want := tt.RevisionName, testRevisionName; got != want {
		t.Errorf("expected %q revisionName got %q", want, got)
	}
	if got, want := tt.ConfigurationName, ""; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValuePinned; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[ela.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}
