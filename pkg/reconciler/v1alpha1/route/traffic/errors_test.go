/*
Copyright 2018 The Knative Author

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package traffic

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestIsFailure_Missing(t *testing.T) {
	err := errMissingRevision("missing-rev")
	want := true
	if got := err.IsFailure(); got != want {
		t.Errorf("wanted %v, got %v", want, got)
	}
}

func TestMarkBadTrafficTarget_Missing(t *testing.T) {
	err := errMissingRevision("missing-rev")
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []duckv1alpha1.ConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &duckv1alpha1.Condition{
			Type:               condType,
			Status:             corev1.ConditionFalse,
			Reason:             "RevisionMissing",
			Message:            `Revision "missing-rev" referenced in traffic not found.`,
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}

func TestIsFailure_NotYetReady(t *testing.T) {
	err := errUnreadyConfiguration(unreadyConfig)
	want := false
	if got := err.IsFailure(); got != want {
		t.Errorf("wanted %v, got %v", want, got)
	}
}

func TestMarkBadTrafficTarget_NotYetReady(t *testing.T) {
	err := errUnreadyConfiguration(unreadyConfig)
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []duckv1alpha1.ConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &duckv1alpha1.Condition{
			Type:               condType,
			Status:             corev1.ConditionUnknown,
			Reason:             "RevisionMissing",
			Message:            `Configuration "unready-config" is waiting for a Revision to become ready.`,
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}

func TestIsFailure_ConfigFailedToBeReady(t *testing.T) {
	err := errUnreadyConfiguration(failedConfig)
	want := true
	if got := err.IsFailure(); got != want {
		t.Errorf("wanted %v, got %v", want, got)
	}
}

func TestMarkBadTrafficTarget_ConfigFailedToBeReady(t *testing.T) {
	err := errUnreadyConfiguration(failedConfig)
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []duckv1alpha1.ConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &duckv1alpha1.Condition{
			Type:               condType,
			Status:             corev1.ConditionFalse,
			Reason:             "RevisionMissing",
			Message:            `Configuration "failed-config" does not have any ready Revision.`,
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}

func TestMarkBadTrafficTarget_RevisionFailedToBeReady(t *testing.T) {
	err := errUnreadyRevision(failedRev)
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []duckv1alpha1.ConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &duckv1alpha1.Condition{
			Type:               condType,
			Status:             corev1.ConditionFalse,
			Reason:             "RevisionMissing",
			Message:            `Revision "failed-revision" failed to become ready.`,
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}

func TestIsFailure_RevFailedToBeReady(t *testing.T) {
	err := errUnreadyRevision(failedRev)
	want := true
	if got := err.IsFailure(); got != want {
		t.Errorf("wanted %v, got %v", want, got)
	}
}

func TestMarkBadTrafficTarget_RevisionNotYetReady(t *testing.T) {
	err := errUnreadyRevision(unreadyRev)
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []duckv1alpha1.ConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &duckv1alpha1.Condition{
			Type:               condType,
			Status:             corev1.ConditionUnknown,
			Reason:             "RevisionMissing",
			Message:            `Revision "unready-revision" is not yet ready.`,
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}
