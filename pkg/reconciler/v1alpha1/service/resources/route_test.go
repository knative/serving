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

package resources

import (
	"testing"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
)

func TestRouteRunLatest(t *testing.T) {
	s := createServiceWithRunLatest()
	testConfigName := names.Configuration(s)
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("expected nil for err got %q", err)
	}
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
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func TestRoutePinned(t *testing.T) {
	s := createServiceWithPinned()
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("expected nil for err got %q", err)
	}
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
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func TestRouteReleaseSingleRevision(t *testing.T) {
	rolloutPercent := 0
	currentPercent := 100
	numRevisions := 1
	s := createServiceWithRelease(numRevisions, rolloutPercent)
	testConfigName := names.Configuration(s)
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("expected %q for service namespace got %q", want, got)
	}
	// Should have 2 named traffic targets: current and latest
	if got, want := len(r.Spec.Traffic), 2; got != want {
		t.Fatalf("expected %d traffic targets, got %d", want, got)
	}
	ttCurrent := r.Spec.Traffic[0]
	if got, want := ttCurrent.Percent, currentPercent; got != want {
		t.Errorf("expected %d percent got %d", want, got)
	}
	if got, want := ttCurrent.Name, "current"; got != want {
		t.Errorf("expected %q name got %q", want, got)
	}
	if got, want := ttCurrent.RevisionName, testRevisionName; got != want {
		t.Errorf("expected %q revisionName got %q", want, got)
	}
	if got, want := ttCurrent.ConfigurationName, ""; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	ttLatest := r.Spec.Traffic[1]
	if got, want := ttLatest.Percent, 0; got != want {
		t.Errorf("expected %d percent got %d", want, got)
	}
	if got, want := ttLatest.Name, "latest"; got != want {
		t.Errorf("expected %q name got %q", want, got)
	}
	if got, want := ttLatest.RevisionName, ""; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	if got, want := ttLatest.ConfigurationName, testConfigName; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValueRelease; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func TestRouteReleaseTwoRevisions(t *testing.T) {
	rolloutPercent := 48
	currentPercent := 52
	numRevisions := 2
	s := createServiceWithRelease(numRevisions, rolloutPercent)
	testConfigName := names.Configuration(s)
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("expected %q for service namespace got %q", want, got)
	}
	// Should have 3 named traffic targets (current, candidate, latest)
	if got, want := len(r.Spec.Traffic), 3; got != want {
		t.Fatalf("expected %d traffic targets, got %d", want, got)
	}
	ttCurrent := r.Spec.Traffic[0]
	if got, want := ttCurrent.Percent, currentPercent; got != want {
		t.Errorf("expected %d percent got %d", want, got)
	}
	if got, want := ttCurrent.Name, "current"; got != want {
		t.Errorf("expected %q name got %q", want, got)
	}
	if got, want := ttCurrent.RevisionName, testRevisionName; got != want {
		t.Errorf("expected %q revisionName got %q", want, got)
	}
	if got, want := ttCurrent.ConfigurationName, ""; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	ttCandidate := r.Spec.Traffic[1]
	if got, want := ttCandidate.Percent, rolloutPercent; got != want {
		t.Errorf("expected %d percent got %d", want, got)
	}
	if got, want := ttCandidate.Name, "candidate"; got != want {
		t.Errorf("expected %q name got %q", want, got)
	}
	if got, want := ttCandidate.RevisionName, testCandidateRevisionName; got != want {
		t.Errorf("expected %q revisionName got %q", want, got)
	}
	if got, want := ttCandidate.ConfigurationName, ""; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	ttLatest := r.Spec.Traffic[2]
	if got, want := ttLatest.Percent, 0; got != want {
		t.Errorf("expected %d percent got %d", want, got)
	}
	if got, want := ttLatest.Name, "latest"; got != want {
		t.Errorf("expected %q name got %q", want, got)
	}
	if got, want := ttLatest.RevisionName, ""; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	if got, want := ttLatest.ConfigurationName, testConfigName; got != want {
		t.Errorf("expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValueRelease; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

// MakeRoute is not called with a ManualType service, but if it is
// called it should produce a route with no traffic targets
func TestRouteManual(t *testing.T) {
	s := createServiceWithManual()
	r, err := MakeRoute(s)
	if r != nil {
		t.Errorf("expected nil for r got %q", err)
	}
	if err == nil {
		t.Errorf("expected err got nil")
	}
}
