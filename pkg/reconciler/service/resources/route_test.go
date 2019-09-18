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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/service/resources/names"
)

func makeRoute(service *v1alpha1.Service) (*v1alpha1.Route, error) {
	// We do this prior to reconciliation, so test with it enabled.
	service.SetDefaults(v1.WithUpgradeViaDefaulting(context.Background()))
	return MakeRoute(service)
}

func TestRouteRunLatest(t *testing.T) {
	s := createServiceWithRunLatest()
	testConfigName := names.Configuration(s)
	r, err := makeRoute(s)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
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
	wantT := []v1alpha1.TrafficTarget{{
		TrafficTarget: v1.TrafficTarget{
			Percent:           ptr.Int64(100),
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
	}}
	if got, want := r.Spec.Traffic, wantT; !cmp.Equal(got, want) {
		t.Errorf("Traffic mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	wantL := map[string]string{
		testLabelKey:            testLabelValueRunLatest,
		serving.ServiceLabelKey: testServiceName,
	}
	if got, want := r.Labels, wantL; !cmp.Equal(got, want) {
		t.Errorf("Labels mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}

	wantA := map[string]string{
		testAnnotationKey: testAnnotationValue,
	}
	if got, want := r.Annotations, wantA; !cmp.Equal(got, want) {
		t.Errorf("Annotations mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
}

func TestRoutePinned(t *testing.T) {
	s := createServiceWithPinned()
	r, err := makeRoute(s)
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
	wantT := []v1alpha1.TrafficTarget{{
		TrafficTarget: v1.TrafficTarget{
			Percent:        ptr.Int64(100),
			RevisionName:   testRevisionName,
			LatestRevision: ptr.Bool(false),
		},
	}}
	if got, want := r.Spec.Traffic, wantT; !cmp.Equal(got, want) {
		t.Errorf("Traffic mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	wantL := map[string]string{
		testLabelKey:            testLabelValuePinned,
		serving.ServiceLabelKey: testServiceName,
	}
	if got, want := r.Labels, wantL; !cmp.Equal(got, want) {
		t.Errorf("Labels mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
}

func TestRouteReleaseSingleRevision(t *testing.T) {
	const numRevisions = 1
	s := createServiceWithRelease(numRevisions, 0 /*no rollout*/)
	testConfigName := names.Configuration(s)
	r, err := makeRoute(s)
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
		TrafficTarget: v1.TrafficTarget{
			Tag:            v1alpha1.CurrentTrafficTarget,
			Percent:        ptr.Int64(100),
			RevisionName:   testRevisionName,
			LatestRevision: ptr.Bool(false),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.LatestTrafficTarget,
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
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

func TestRouteLatestRevisionSplit(t *testing.T) {
	const (
		rolloutPercent = 42
		currentPercent = 100 - rolloutPercent
	)
	s := createServiceWithRelease(2 /*num revisions*/, rolloutPercent)
	s.Spec.DeprecatedRelease.Revisions = []string{v1alpha1.ReleaseLatestRevisionKeyword, "juicy-revision"}
	testConfigName := names.Configuration(s)
	r, err := makeRoute(s)
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
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.CurrentTrafficTarget,
			Percent:           ptr.Int64(currentPercent),
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:            v1alpha1.CandidateTrafficTarget,
			Percent:        ptr.Int64(rolloutPercent),
			RevisionName:   "juicy-revision",
			LatestRevision: ptr.Bool(false),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.LatestTrafficTarget,
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
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
	s.Spec.DeprecatedRelease.Revisions = []string{"squishy-revision", v1alpha1.ReleaseLatestRevisionKeyword}
	testConfigName := names.Configuration(s)
	r, err := makeRoute(s)
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
		TrafficTarget: v1.TrafficTarget{
			Tag:            v1alpha1.CurrentTrafficTarget,
			Percent:        ptr.Int64(currentPercent),
			RevisionName:   "squishy-revision",
			LatestRevision: ptr.Bool(false),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.CandidateTrafficTarget,
			Percent:           ptr.Int64(rolloutPercent),
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.LatestTrafficTarget,
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
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
	s.Spec.DeprecatedRelease.Revisions = []string{v1alpha1.ReleaseLatestRevisionKeyword}
	testConfigName := names.Configuration(s)
	r, err := makeRoute(s)

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
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.CurrentTrafficTarget,
			Percent:           ptr.Int64(100),
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.LatestTrafficTarget,
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
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
	const (
		currentPercent = 52
		numRevisions   = 2
	)
	s := createServiceWithRelease(numRevisions, 100-currentPercent)
	testConfigName := names.Configuration(s)
	r, err := makeRoute(s)
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
	wantT := []v1alpha1.TrafficTarget{{
		TrafficTarget: v1.TrafficTarget{
			Tag:            v1alpha1.CurrentTrafficTarget,
			Percent:        ptr.Int64(currentPercent),
			RevisionName:   testRevisionName,
			LatestRevision: ptr.Bool(false),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:            v1alpha1.CandidateTrafficTarget,
			Percent:        ptr.Int64(100 - currentPercent),
			RevisionName:   testCandidateRevisionName,
			LatestRevision: ptr.Bool(false),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			Tag:               v1alpha1.LatestTrafficTarget,
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
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

func TestInlineRouteSpec(t *testing.T) {
	s := createServiceInline()
	testConfigName := names.Configuration(s)
	r, err := makeRoute(s)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
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
	wantT := []v1alpha1.TrafficTarget{{
		TrafficTarget: v1.TrafficTarget{
			Percent:           ptr.Int64(100),
			ConfigurationName: testConfigName,
			LatestRevision:    ptr.Bool(true),
		},
	}}
	if got, want := r.Spec.Traffic, wantT; !cmp.Equal(got, want) {
		t.Errorf("Traffic mismatch: diff (-got, +want): %s", cmp.Diff(got, want))
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 1; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func TestRouteHasNoKubectlAnnotation(t *testing.T) {
	s := createServiceWithKubectlAnnotation()
	r, err := makeRoute(s)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if v, ok := r.Annotations[corev1.LastAppliedConfigAnnotation]; ok {
		t.Errorf("Annotation %s = %q, want empty", corev1.LastAppliedConfigAnnotation, v)
	}
}
