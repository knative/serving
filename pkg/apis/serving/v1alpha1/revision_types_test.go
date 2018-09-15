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
package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	sapis "github.com/knative/serving/pkg/apis"
)

func TestGeneration(t *testing.T) {
	r := Revision{}
	if a := r.GetGeneration(); a != 0 {
		t.Errorf("empty revision generation should be 0 was: %d", a)
	}

	r.SetGeneration(5)
	if e, a := int64(5), r.GetGeneration(); e != a {
		t.Errorf("getgeneration mismatch expected: %d got: %d", e, a)
	}

}

func TestIsActivationRequired(t *testing.T) {
	cases := []struct {
		name                 string
		status               RevisionStatus
		isActivationRequired bool
	}{{
		name:                 "empty status should not be inactive",
		status:               RevisionStatus{},
		isActivationRequired: false,
	}, {
		name: "Ready status should not be inactive",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isActivationRequired: false,
	}, {
		name: "Inactive status should be inactive",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionActive,
				Status: corev1.ConditionFalse,
			}},
		},
		isActivationRequired: true,
	}, {
		name: "Updating status should be inactive",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionUnknown,
				Reason: "Updating",
			}, {
				Type:   RevisionConditionActive,
				Status: corev1.ConditionUnknown,
				Reason: "Updating",
			}},
		},
		isActivationRequired: true,
	}, {
		name: "NotReady status without reason should not be inactive",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isActivationRequired: false,
	}, {
		name: "Ready/Unknown status without reason should not be inactive",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
		isActivationRequired: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e, a := tc.isActivationRequired, tc.status.IsActivationRequired(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}
		})
	}
}

func TestIsRoutable(t *testing.T) {
	cases := []struct {
		name       string
		status     RevisionStatus
		isRoutable bool
	}{{
		name:       "empty status should not be routable",
		status:     RevisionStatus{},
		isRoutable: false,
	}, {
		name: "Ready status should be routable",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isRoutable: true,
	}, {
		name: "Inactive status should be routable",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionActive,
				Status: corev1.ConditionFalse,
			}, {
				Type:   RevisionConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isRoutable: true,
	}, {
		name: "NotReady status without reason should not be routable",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isRoutable: false,
	}, {
		name: "Ready/Unknown status without reason should not be routable",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
		isRoutable: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.isRoutable, tc.status.IsRoutable(); got != want {
				t.Errorf("%s: IsRoutable() = %v want: %v", tc.name, got, want)
			}
		})
	}
}

func TestIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  RevisionStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  RevisionStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionBuildSucceeded,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type: RevisionConditionReady,
			}},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionBuildSucceeded,
				Status: corev1.ConditionTrue,
			}, {
				Type:   RevisionConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: RevisionStatus{
			Conditions: sapis.Conditions{{
				Type:   RevisionConditionBuildSucceeded,
				Status: corev1.ConditionTrue,
			}, {
				Type:   RevisionConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
		isReady: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e, a := tc.isReady, tc.status.IsReady(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}
		})
	}
}

func TestGetSetCondition(t *testing.T) {
	rs := RevisionStatus{}
	if a := rs.GetCondition(RevisionConditionReady); a != nil {
		t.Errorf("empty RevisionStatus returned %v when expected nil", a)
	}

	rc := &sapis.Condition{
		Type:   RevisionConditionBuildSucceeded,
		Status: corev1.ConditionTrue,
	}
	// Set Condition and make sure it's the only thing returned
	rs.setCondition(rc)

	if diff := cmp.Diff(rc, rs.GetCondition(RevisionConditionBuildSucceeded), cmpopts.IgnoreFields(sapis.Condition{}, "LastTransitionTime")); diff != "" {
		t.Errorf("GetCondition refs diff (-want +got): %v", diff)
	}
	if a := rs.GetCondition(RevisionConditionReady); a != nil {
		t.Errorf("GetCondition expected nil got: %v", a)
	}
}

func TestRevisionConditions(t *testing.T) {
	rev := &Revision{}
	foo := &sapis.Condition{
		Type:   "Foo",
		Status: "True",
	}
	bar := &sapis.Condition{
		Type:   "Bar",
		Status: "True",
	}

	// Add a new condition.
	rev.Status.setCondition(foo)

	if got, want := len(rev.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nothing
	rev.Status.setCondition(nil)

	if got, want := len(rev.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add a second condition.
	rev.Status.setCondition(bar)

	if got, want := len(rev.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nil condition.
	rev.Status.setCondition(nil)

	if got, want := len(rev.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}
}

func TestTypicalFlowWithBuild(t *testing.T) {
	r := &Revision{}
	r.Status.InitializeConditions()
	r.Status.InitializeBuildCondition()
	checkConditionOngoingRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	// Empty BuildStatus keeps things as-is.
	r.Status.PropagateBuildStatus(buildv1alpha1.BuildStatus{})
	checkConditionOngoingRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	r.Status.PropagateBuildStatus(buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{{
			Type:   buildv1alpha1.BuildSucceeded,
			Status: corev1.ConditionUnknown,
		}},
	})
	want := "Building"
	if got := checkConditionOngoingRevision(r.Status, RevisionConditionBuildSucceeded, t); got == nil || got.Reason != want {
		t.Errorf("PropagateBuildStatus(Unknown) = %v, wanted %v", got, want)
	}
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	if got := checkConditionOngoingRevision(r.Status, RevisionConditionReady, t); got == nil || got.Reason != want {
		t.Errorf("PropagateBuildStatus(Unknown) = %v, wanted %v", got, want)
	}

	r.Status.PropagateBuildStatus(buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{{
			Type:   buildv1alpha1.BuildSucceeded,
			Status: corev1.ConditionTrue,
		}},
	})
	checkConditionSucceededRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	// All of these conditions should get this status.
	want = "TheReason"
	r.Status.MarkDeploying(want)
	checkConditionSucceededRevision(r.Status, RevisionConditionBuildSucceeded, t)
	if got := checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t); got == nil || got.Reason != want {
		t.Errorf("MarkDeploying = %v, wanted %v", got, want)
	}
	if got := checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t); got == nil || got.Reason != want {
		t.Errorf("MarkDeploying = %v, wanted %v", got, want)
	}
	if got := checkConditionOngoingRevision(r.Status, RevisionConditionReady, t); got == nil || got.Reason != want {
		t.Errorf("MarkDeploying = %v, wanted %v", got, want)
	}

	r.Status.MarkActive()
	checkConditionSucceededRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionActive, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	if r.Status.IsReady() {
		t.Error("IsReady() = true, want false")
	}

	r.Status.MarkContainerHealthy()
	checkConditionSucceededRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionActive, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	if r.Status.IsReady() {
		t.Error("IsReady() = true, want false")
	}

	r.Status.MarkResourcesAvailable()
	checkConditionSucceededRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionActive, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionReady, t)

	if !r.Status.IsReady() {
		t.Error("IsReady() = false, want true")
	}

	// Verify that this doesn't reset our conditions.
	r.Status.InitializeConditions()
	checkConditionSucceededRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionActive, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionReady, t)

	// Or this.
	r.Status.InitializeBuildCondition()
	checkConditionSucceededRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionReady, t)
}

func TestTypicalFlowWithBuildFailure(t *testing.T) {
	r := &Revision{}
	r.Status.InitializeConditions()
	r.Status.InitializeBuildCondition()
	checkConditionOngoingRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	r.Status.PropagateBuildStatus(buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{{
			Type:   buildv1alpha1.BuildSucceeded,
			Status: corev1.ConditionUnknown,
		}},
	})
	checkConditionOngoingRevision(r.Status, RevisionConditionBuildSucceeded, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	wantReason, wantMessage := "this is the reason", "and this the message"
	r.Status.PropagateBuildStatus(buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{{
			Type:    buildv1alpha1.BuildSucceeded,
			Status:  corev1.ConditionFalse,
			Reason:  wantReason,
			Message: wantMessage,
		}},
	})
	if got := checkConditionFailedRevision(r.Status, RevisionConditionBuildSucceeded, t); got == nil {
		t.Errorf("MarkBuildFailed = nil, wanted %v", wantReason)
	} else if got.Reason != wantReason {
		t.Errorf("MarkBuildFailed = %v, wanted %v", got.Reason, wantReason)
	} else if got.Message != wantMessage {
		t.Errorf("MarkBuildFailed = %v, wanted %v", got.Reason, wantMessage)
	}
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	if got := checkConditionFailedRevision(r.Status, RevisionConditionReady, t); got == nil {
		t.Errorf("MarkBuildFailed = nil, wanted %v", wantReason)
	} else if got.Reason != wantReason {
		t.Errorf("MarkBuildFailed = %v, wanted %v", got.Reason, wantReason)
	} else if got.Message != wantMessage {
		t.Errorf("MarkBuildFailed = %v, wanted %v", got.Reason, wantMessage)
	}
}

func TestTypicalFlowWithServiceTimeout(t *testing.T) {
	r := &Revision{}
	r.Status.InitializeConditions()
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	r.Status.MarkServiceTimeout()
	checkConditionFailedRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionFailedRevision(r.Status, RevisionConditionReady, t)
}

func TestTypicalFlowWithProgressDeadlineExceeded(t *testing.T) {
	r := &Revision{}
	r.Status.InitializeConditions()
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	want := "the error message"
	r.Status.MarkProgressDeadlineExceeded(want)
	if got := checkConditionFailedRevision(r.Status, RevisionConditionResourcesAvailable, t); got == nil || got.Message != want {
		t.Errorf("MarkProgressDeadlineExceeded = %v, want %v", got, want)
	}
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	if got := checkConditionFailedRevision(r.Status, RevisionConditionReady, t); got == nil || got.Message != want {
		t.Errorf("MarkProgressDeadlineExceeded = %v, want %v", got, want)
	}
}

func TestTypicalFlowWithContainerMissing(t *testing.T) {
	r := &Revision{}
	r.Status.InitializeConditions()
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)

	want := "something about the container being not found"
	r.Status.MarkContainerMissing(want)
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	if got := checkConditionFailedRevision(r.Status, RevisionConditionContainerHealthy, t); got == nil || got.Message != want {
		t.Errorf("MarkContainerMissing = %v, want %v", got, want)
	} else if got.Reason != "ContainerMissing" {
		t.Errorf("MarkContainerMissing = %v, want %v", got, "ContainerMissing")
	}
	if got := checkConditionFailedRevision(r.Status, RevisionConditionReady, t); got == nil || got.Message != want {
		t.Errorf("MarkContainerMissing = %v, want %v", got, want)
	} else if got.Reason != "ContainerMissing" {
		t.Errorf("MarkContainerMissing = %v, want %v", got, "ContainerMissing")
	}
}

func TestTypicalFlowWithSuspendResume(t *testing.T) {
	r := &Revision{}
	r.Status.InitializeConditions()
	checkConditionOngoingRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionReady, t)
	checkConditionOngoingRevision(r.Status, RevisionConditionActive, t)

	// Enter a Ready state.
	r.Status.MarkActive()
	r.Status.MarkContainerHealthy()
	r.Status.MarkResourcesAvailable()
	checkConditionSucceededRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionActive, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionReady, t)

	// From a Ready state, make the revision inactive to simulate scale to zero.
	want := "Deactivated"
	r.Status.MarkInactive(want, "Reserve")
	checkConditionSucceededRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	if got := checkConditionFailedRevision(r.Status, RevisionConditionActive, t); got == nil || got.Reason != want {
		t.Errorf("MarkInactive = %v, want %v", got, want)
	}
	if got := checkConditionFailedRevision(r.Status, RevisionConditionReady, t); got == nil || got.Reason != want {
		t.Errorf("MarkInactive = %v, want %v", got, want)
	}

	// From an Inactive state, start to activate the revision.
	want = "Activating"
	r.Status.MarkActivating(want, "blah blah blah")
	checkConditionSucceededRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	if got := checkConditionOngoingRevision(r.Status, RevisionConditionActive, t); got == nil || got.Reason != want {
		t.Errorf("MarkInactive = %v, want %v", got, want)
	}
	if got := checkConditionOngoingRevision(r.Status, RevisionConditionReady, t); got == nil || got.Reason != want {
		t.Errorf("MarkInactive = %v, want %v", got, want)
	}

	// From the activating state, simulate the transition back to readiness.
	r.Status.MarkActive()
	checkConditionSucceededRevision(r.Status, RevisionConditionResourcesAvailable, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionContainerHealthy, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionActive, t)
	checkConditionSucceededRevision(r.Status, RevisionConditionReady, t)
}

func checkConditionSucceededRevision(rs RevisionStatus, rct sapis.ConditionType, t *testing.T) *sapis.Condition {
	t.Helper()
	return checkConditionRevision(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedRevision(rs RevisionStatus, rct sapis.ConditionType, t *testing.T) *sapis.Condition {
	t.Helper()
	return checkConditionRevision(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingRevision(rs RevisionStatus, rct sapis.ConditionType, t *testing.T) *sapis.Condition {
	t.Helper()
	return checkConditionRevision(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionRevision(rs RevisionStatus, rct sapis.ConditionType, cs corev1.ConditionStatus, t *testing.T) *sapis.Condition {
	t.Helper()
	r := rs.GetCondition(rct)
	if r == nil {
		t.Fatalf("Get(%v) = nil, wanted %v=%v", rct, rct, cs)
	}
	if r.Status != cs {
		t.Fatalf("Get(%v) = %v, wanted %v", rct, r.Status, cs)
	}
	return r
}

func TestRevisionGetGroupVersionKind(t *testing.T) {
	r := &Revision{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1alpha1",
		Kind:    "Revision",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
