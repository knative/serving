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

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
)

func TestRouteRunLatest(t *testing.T) {
	s := createServiceWithRunLatest()
	testConfigName := names.Configuration(s)
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("UnExpected error: %v", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	if got, want := len(r.Spec.Traffic), 1; got != want {
		t.Fatalf("Expected %d traffic targets got %d", want, got)
	}
	tt := r.Spec.Traffic[0]
	if got, want := tt.Percent, 100; got != want {
		t.Errorf("Expected %d percent got %d", want, got)
	}
	if got, want := tt.RevisionName, ""; got != want {
		t.Errorf("Expected %q revisionName got %q", want, got)
	}
	if got, want := tt.ConfigurationName, testConfigName; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("Expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValueRunLatest; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
	}
}

func TestRoutePinned(t *testing.T) {
	s := createServiceWithPinned()
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("Expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	if got, want := len(r.Spec.Traffic), 1; got != want {
		t.Fatalf("Expected %d traffic targets, got %d", want, got)
	}
	tt := r.Spec.Traffic[0]
	if got, want := tt.Percent, 100; got != want {
		t.Errorf("Expected %d percent got %d", want, got)
	}
	if got, want := tt.RevisionName, testRevisionName; got != want {
		t.Errorf("Expected %q revisionName got %q", want, got)
	}
	if got, want := tt.ConfigurationName, ""; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("Expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValuePinned; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
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
		t.Errorf("Expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	// Should have 2 named traffic targets: current and latest
	if got, want := len(r.Spec.Traffic), 2; got != want {
		t.Fatalf("Expected %d traffic targets, got %d", want, got)
	}
	ttCurrent := r.Spec.Traffic[0]
	if got, want := ttCurrent.Percent, currentPercent; got != want {
		t.Errorf("Expected %d percent got %d", want, got)
	}
	if got, want := ttCurrent.Name, v1alpha1.CurrentTrafficTarget; got != want {
		t.Errorf("Expected %q name got %q", want, got)
	}
	if got, want := ttCurrent.RevisionName, testRevisionName; got != want {
		t.Errorf("Expected %q revisionName got %q", want, got)
	}
	if got, want := ttCurrent.ConfigurationName, ""; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	ttLatest := r.Spec.Traffic[1]
	if got, want := ttLatest.Percent, 0; got != want {
		t.Errorf("Expected %d percent got %d", want, got)
	}
	if got, want := ttLatest.Name, v1alpha1.LatestTrafficTarget; got != want {
		t.Errorf("Expected %q name got %q", want, got)
	}
	if got, want := ttLatest.RevisionName, ""; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	if got, want := ttLatest.ConfigurationName, testConfigName; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("Expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValueRelease; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
	}
}

func TestRouteLatestRevisionSplit(t *testing.T) {
	const (
		rolloutPercent = 42
		currentPercent = 100 - rolloutPercent
	)
	s := createServiceWithRelease(2 /*num revisions*/, rolloutPercent)
	s.Spec.Release.Revisions = []string{v1alpha1.ReleaseLatestRevisionKeyword, "juicy-revision"}
	testConfigName := names.Configuration(s)
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("Expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	wantT := []v1alpha1.TrafficTarget{{
		Name:              v1alpha1.CurrentTrafficTarget,
		Percent:           currentPercent,
		ConfigurationName: testConfigName,
	}, {
		Name:         v1alpha1.CandidateTrafficTarget,
		Percent:      rolloutPercent,
		RevisionName: "juicy-revision",
	}, {
		Name:              v1alpha1.LatestTrafficTarget,
		ConfigurationName: testConfigName,
	}}
	if got, want := r.Spec.Traffic, wantT; !cmp.Equal(got, want) {
		t.Errorf("Traffic mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	wantL := map[string]string{
		testLabelKey:            testLabelValueRelease,
		serving.ServiceLabelKey: testServiceName,
	}
	if got, want := r.Labels, wantL; !cmp.Equal(got, want) {
		t.Errorf("Labels mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
}
func TestRouteLatestRevisionSplitCandidate(t *testing.T) {
	const (
		rolloutPercent = 42
		currentPercent = 100 - rolloutPercent
	)
	s := createServiceWithRelease(2 /*num revisions*/, rolloutPercent)
	s.Spec.Release.Revisions = []string{"squishy-revision", v1alpha1.ReleaseLatestRevisionKeyword}
	testConfigName := names.Configuration(s)
	r, err := MakeRoute(s)
	if err != nil {
		t.Errorf("Expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	wantT := []v1alpha1.TrafficTarget{{
		Name:         v1alpha1.CurrentTrafficTarget,
		Percent:      currentPercent,
		RevisionName: "squishy-revision",
	}, {
		Name:              v1alpha1.CandidateTrafficTarget,
		Percent:           rolloutPercent,
		ConfigurationName: testConfigName,
	}, {
		Name:              v1alpha1.LatestTrafficTarget,
		ConfigurationName: testConfigName,
	}}
	if got, want := r.Spec.Traffic, wantT; !cmp.Equal(got, want) {
		t.Errorf("Traffic mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	wantL := map[string]string{
		testLabelKey:            testLabelValueRelease,
		serving.ServiceLabelKey: testServiceName,
	}
	if got, want := r.Labels, wantL; !cmp.Equal(got, want) {
		t.Errorf("Labels mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
}
func TestRouteLatestRevisionNoSplit(t *testing.T) {
	s := createServiceWithRelease(1 /*num revisions*/, 0 /*unused*/)
	s.Spec.Release.Revisions = []string{v1alpha1.ReleaseLatestRevisionKeyword}
	testConfigName := names.Configuration(s)
	r, err := MakeRoute(s)

	if err != nil {
		t.Errorf("Expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	// Should have 2 named traffic targets (current, latest)
	wantT := []v1alpha1.TrafficTarget{{
		Name:              v1alpha1.CurrentTrafficTarget,
		Percent:           100,
		ConfigurationName: testConfigName,
	}, {
		Name:              v1alpha1.LatestTrafficTarget,
		ConfigurationName: testConfigName,
	}}
	if got, want := r.Spec.Traffic, wantT; !cmp.Equal(got, want) {
		t.Errorf("Traffic mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	wantL := map[string]string{
		testLabelKey:            testLabelValueRelease,
		serving.ServiceLabelKey: testServiceName,
	}
	if got, want := r.Labels, wantL; !cmp.Equal(got, want) {
		t.Errorf("Labels mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
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
		t.Errorf("Expected nil for err got %q", err)
	}
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	// Should have 3 named traffic targets (current, candidate, latest)
	if got, want := len(r.Spec.Traffic), 3; got != want {
		t.Fatalf("Expected %d traffic targets, got %d", want, got)
	}
	ttCurrent := r.Spec.Traffic[0]
	if got, want := ttCurrent.Percent, currentPercent; got != want {
		t.Errorf("Expected %d percent got %d", want, got)
	}
	if got, want := ttCurrent.Name, v1alpha1.CurrentTrafficTarget; got != want {
		t.Errorf("Expected %q name got %q", want, got)
	}
	if got, want := ttCurrent.RevisionName, testRevisionName; got != want {
		t.Errorf("Expected %q revisionName got %q", want, got)
	}
	if got, want := ttCurrent.ConfigurationName, ""; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	ttCandidate := r.Spec.Traffic[1]
	if got, want := ttCandidate.Percent, rolloutPercent; got != want {
		t.Errorf("Expected %d percent got %d", want, got)
	}
	if got, want := ttCandidate.Name, v1alpha1.CandidateTrafficTarget; got != want {
		t.Errorf("Expected %q name got %q", want, got)
	}
	if got, want := ttCandidate.RevisionName, testCandidateRevisionName; got != want {
		t.Errorf("Expected %q revisionName got %q", want, got)
	}
	if got, want := ttCandidate.ConfigurationName, ""; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	ttLatest := r.Spec.Traffic[2]
	if got, want := ttLatest.Percent, 0; got != want {
		t.Errorf("Expected %d percent got %d", want, got)
	}
	if got, want := ttLatest.Name, v1alpha1.LatestTrafficTarget; got != want {
		t.Errorf("Expected %q name got %q", want, got)
	}
	if got, want := ttLatest.RevisionName, ""; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	if got, want := ttLatest.ConfigurationName, testConfigName; got != want {
		t.Errorf("Expected %q configurationname got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 2; got != want {
		t.Errorf("Expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[testLabelKey], testLabelValueRelease; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("Expected %q labels got %q", want, got)
	}
}

// MakeRoute is not called with a ManualType service, but if it is
// called it should produce a route with no traffic targets
func TestRouteManual(t *testing.T) {
	s := createServiceWithManual()
	r, err := MakeRoute(s)
	if r != nil {
		t.Errorf("Expected nil for r got %q", err)
	}
	if err == nil {
		t.Error("Expected err got nil")
	}
}
