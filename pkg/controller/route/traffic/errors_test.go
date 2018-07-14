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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestMarkBadTrafficTarget_Missing(t *testing.T) {
	err := errMissingRevision("missing-rev")
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []v1alpha1.RouteConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &v1alpha1.RouteCondition{
			Type:               condType,
			Status:             corev1.ConditionFalse,
			Reason:             "RevisionMissing",
			Message:            `Revision "missing-rev" referenced in traffic not found.`,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}

func TestMarkBadTrafficTarget_Deleted(t *testing.T) {
	err := errDeletedRevision("my-latest-rev-was-deleted")
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []v1alpha1.RouteConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &v1alpha1.RouteCondition{
			Type:               condType,
			Status:             corev1.ConditionFalse,
			Reason:             "RevisionMissing",
			Message:            `Latest Revision of Configuration "my-latest-rev-was-deleted" is deleted.`,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}

func TestMarkBadTrafficTarget_NeverReady(t *testing.T) {
	err := errUnreadyConfiguration("i-was-never-ready")
	r := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})

	err.MarkBadTrafficTarget(&r.Status)
	for _, condType := range []v1alpha1.RouteConditionType{
		v1alpha1.RouteConditionAllTrafficAssigned,
		v1alpha1.RouteConditionReady,
	} {
		got := r.Status.GetCondition(condType)
		want := &v1alpha1.RouteCondition{
			Type:               condType,
			Status:             corev1.ConditionUnknown,
			Reason:             "RevisionMissing",
			Message:            `Configuration "i-was-never-ready" does not have a LatestReadyRevision.`,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected condition diff (-want +got): %v", diff)
		}
	}
}
