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
package v1

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apistest "knative.dev/pkg/apis/testing"
	"knative.dev/pkg/ptr"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/config"
)

func TestRevisionDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Revision{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Revision, %T) = %v", test.t, err)
			}
		})
	}
}

func TestRevisionGetConditionSet(t *testing.T) {
	r := &Revision{}

	if got, want := r.GetConditionSet().GetTopLevelConditionType(), apis.ConditionReady; got != want {
		t.Errorf("GetTopLevelCondition=%v, want=%v", got, want)
	}
}

func TestRevisionGetGroupVersionKind(t *testing.T) {
	r := &Revision{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1",
		Kind:    "Revision",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("GVK: %v, want: %v", got, want)
	}
}

func TestGetContainerConcurrency(t *testing.T) {
	tests := []struct {
		name     string
		rs       *RevisionSpec
		expected int64
	}{{
		name:     "nil concurrency",
		rs:       &RevisionSpec{},
		expected: config.DefaultContainerConcurrency,
	}, {
		name:     "concurrency 42",
		rs:       &RevisionSpec{ContainerConcurrency: ptr.Int64(42)},
		expected: 42,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cc := test.rs.GetContainerConcurrency()
			if cc != test.expected {
				t.Errorf("GetContainerConcurrency() = %d, expected:%d", cc, test.expected)
			}
		})
	}

}

func TestRevisionIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  RevisionStatus
		isReady bool
	}{{
		name:   "empty status should not be ready",
		status: RevisionStatus{},
	}, {
		name: "Different condition type should not be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionResourcesAvailable,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	}, {
		name: "False condition status should not be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
	}, {
		name: "Unknown condition status should not be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
	}, {
		name: "Missing condition status should not be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: RevisionConditionReady,
				}},
			},
		},
	}, {
		name: "True condition status should be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionResourcesAvailable,
					Status: corev1.ConditionTrue,
				}, {
					Type:   RevisionConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionResourcesAvailable,
					Status: corev1.ConditionTrue,
				}, {
					Type:   RevisionConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Revision{Status: tc.status}
			if got, want := r.IsReady(), tc.isReady; got != want {
				t.Errorf("isReady =  %v want: %v", got, want)
			}
		})
	}
}

func TestRevisionIsFailed(t *testing.T) {
	cases := []struct {
		name     string
		status   RevisionStatus
		isFailed bool
	}{{
		name:     "empty status should not be failed",
		status:   RevisionStatus{},
		isFailed: false,
	}, {
		name: "False condition status should be failed",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isFailed: true,
	}, {
		name: "Unknown condition status should not be failed",
		status: RevisionStatus{
			Status: duckv1.Status{

				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isFailed: false,
	}, {
		name: "Missing condition status should not be failed",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: RevisionConditionReady,
				}},
			},
		},
		isFailed: false,
	}, {
		name: "True condition status should not be failed",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isFailed: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Revision{Status: tc.status}
			if e, a := tc.isFailed, r.IsFailed(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}
		})
	}
}

func TestRevisionInitializeConditions(t *testing.T) {
	rs := &RevisionStatus{}
	rs.InitializeConditions()

	types := make([]string, 0, len(rs.Conditions))
	for _, cond := range rs.Conditions {
		types = append(types, string(cond.Type))
	}

	// These are already sorted.
	expected := []string{
		string(RevisionConditionContainerHealthy),
		string(apis.ConditionReady),
		string(RevisionConditionResourcesAvailable),
	}

	sort.Strings(types)

	if diff := cmp.Diff(expected, types); diff != "" {
		t.Error("Conditions(-want,+got):\n", diff)
	}
}

func TestTypicalFlowWithProgressDeadlineExceeded(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionOngoing(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	const want = "the error message"
	r.MarkResourcesAvailableFalse(ReasonProgressDeadlineExceeded, want)
	apistest.CheckConditionFailed(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionFailed(r, RevisionConditionReady, t)
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Message != want {
		t.Errorf("MarkProgressDeadlineExceeded = %q, want %q", got, want)
	}
	if got := r.GetCondition(RevisionConditionReady); got == nil || got.Message != want {
		t.Errorf("MarkProgressDeadlineExceeded = %q, want %q", got, want)
	}
}

func TestTypicalFlowWithContainerMissing(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionOngoing(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	const want = "something about the container being not found"
	r.MarkContainerHealthyFalse(ReasonContainerMissing, want)
	apistest.CheckConditionOngoing(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionFailed(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionFailed(r, RouteConditionReady, t)
	if got := r.GetCondition(RevisionConditionContainerHealthy); got == nil || got.Message != want {
		t.Errorf("MarkContainerMissing = %v, want %v", got, want)
	} else if got.Reason != "ContainerMissing" {
		t.Errorf("MarkContainerMissing = %q, want %q", got, "ContainerMissing")
	}
	if got := r.GetCondition(RevisionConditionReady); got == nil || got.Message != want {
		t.Errorf("MarkContainerMissing = %v, want %v", got, want)
	} else if got.Reason != "ContainerMissing" {
		t.Errorf("MarkContainerMissing = %q, want %q", got, "ContainerMissing")
	}

	r.MarkContainerHealthyUnknown(ReasonDeploying, want)
	apistest.CheckConditionOngoing(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)
}

func TestTypicalFlowWithSuspendResume(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionOngoing(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	// Enter a Ready state.
	r.MarkActiveTrue()
	r.MarkContainerHealthyTrue()
	r.MarkResourcesAvailableTrue()
	apistest.CheckConditionSucceeded(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)

	// From a Ready state, make the revision inactive to simulate scale to zero.
	const want = "Deactivated"
	r.MarkActiveFalse(want, "Reserve")
	apistest.CheckConditionSucceeded(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionFailed(r, RevisionConditionActive, t)
	if got := r.GetCondition(RevisionConditionActive); got == nil || got.Reason != want {
		t.Errorf("MarkInactive = %q, want %q", got, want)
	}
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)

	// From an Inactive state, start to activate the revision.
	const want2 = "Activating"
	r.MarkActiveUnknown(want2, "blah blah blah")
	apistest.CheckConditionSucceeded(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(r, RevisionConditionActive, t)
	if got := r.GetCondition(RevisionConditionActive); got == nil || got.Reason != want2 {
		t.Errorf("MarkInactive = %q, want %q", got, want2)
	}
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)

	// From the activating state, simulate the transition back to readiness.
	r.MarkActiveTrue()
	apistest.CheckConditionSucceeded(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)
}

func TestRevisionNotOwnedStuff(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionOngoing(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	const want = "NotOwned"
	r.MarkResourcesAvailableFalse(ReasonNotOwned, "mark")
	apistest.CheckConditionFailed(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionFailed(r, RevisionConditionReady, t)
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %q, want %q", got, want)
	}
	if got := r.GetCondition(RevisionConditionReady); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %q, want %q", got, want)
	}
}

func TestRevisionResourcesUnavailable(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionOngoing(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	const wantReason, wantMessage = "unschedulable", "insufficient energy"
	r.MarkResourcesAvailableFalse(wantReason, wantMessage)
	apistest.CheckConditionFailed(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionFailed(r, RevisionConditionReady, t)
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Reason != wantReason {
		t.Errorf("RevisionConditionResourcesAvailable.Reason = %q, want %q", got, wantReason)
	}
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Message != wantMessage {
		t.Errorf("RevisionConditionResourcesAvailable.Message = %q, want %q", got, wantMessage)
	}

	r.MarkResourcesAvailableUnknown(wantReason, wantMessage)
	apistest.CheckConditionOngoing(r, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)
}

func TestPropagateDeploymentStatus(t *testing.T) {
	rev := &RevisionStatus{}
	rev.InitializeConditions()

	// We start out ongoing.
	apistest.CheckConditionOngoing(rev, RevisionConditionReady, t)
	apistest.CheckConditionOngoing(rev, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(rev, RevisionConditionResourcesAvailable, t)

	// Empty deployment conditions shouldn't affect our readiness.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{},
	})
	apistest.CheckConditionOngoing(rev, RevisionConditionReady, t)
	apistest.CheckConditionOngoing(rev, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionOngoing(rev, RevisionConditionResourcesAvailable, t)

	// Deployment failures should be propagated and not affect ContainerHealthy.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionFalse,
		}, {
			Type:   appsv1.DeploymentReplicaFailure,
			Status: corev1.ConditionUnknown,
		}},
	})
	apistest.CheckConditionFailed(rev, RevisionConditionReady, t)
	apistest.CheckConditionFailed(rev, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionOngoing(rev, RevisionConditionContainerHealthy, t)

	// Marking container healthy doesn't affect deployment status.
	rev.MarkContainerHealthyTrue()
	apistest.CheckConditionFailed(rev, RevisionConditionReady, t)
	apistest.CheckConditionFailed(rev, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionContainerHealthy, t)

	// We can recover from deployment failures.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionTrue,
		}},
	})
	apistest.CheckConditionSucceeded(rev, RevisionConditionReady, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionContainerHealthy, t)

	// We can go unknown.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionUnknown,
		}},
	})
	apistest.CheckConditionOngoing(rev, RevisionConditionReady, t)
	apistest.CheckConditionOngoing(rev, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionContainerHealthy, t)

	// ReplicaFailure=True translates into Ready=False.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentReplicaFailure,
			Status: corev1.ConditionTrue,
		}},
	})
	apistest.CheckConditionFailed(rev, RevisionConditionReady, t)
	apistest.CheckConditionFailed(rev, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionContainerHealthy, t)

	// ReplicaFailure=True trumps Progressing=Unknown.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionUnknown,
		}, {
			Type:   appsv1.DeploymentReplicaFailure,
			Status: corev1.ConditionTrue,
		}},
	})
	apistest.CheckConditionFailed(rev, RevisionConditionReady, t)
	apistest.CheckConditionFailed(rev, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionContainerHealthy, t)

	// ReplicaFailure=False + Progressing=True yields Ready.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionTrue,
		}, {
			Type:   appsv1.DeploymentReplicaFailure,
			Status: corev1.ConditionFalse,
		}},
	})
	apistest.CheckConditionSucceeded(rev, RevisionConditionReady, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionResourcesAvailable, t)
	apistest.CheckConditionSucceeded(rev, RevisionConditionContainerHealthy, t)
}

func TestPropagateAutoscalerStatus(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	// PodAutoscaler has no active condition, so we are just coming up.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{},
	})
	apistest.CheckConditionOngoing(r, RevisionConditionActive, t)

	// PodAutoscaler becomes ready, making us active.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   av1alpha1.PodAutoscalerConditionScaleTargetInitialized,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(r, RevisionConditionActive, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)

	// PodAutoscaler flipping back to Unknown causes Active become ongoing immediately.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionUnknown,
			}, {
				Type:   av1alpha1.PodAutoscalerConditionScaleTargetInitialized,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionOngoing(r, RevisionConditionActive, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)

	// PodAutoscaler becoming unready makes Active false, but doesn't affect readiness.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}, {
				Type:   av1alpha1.PodAutoscalerConditionScaleTargetInitialized,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionFailed(r, RevisionConditionActive, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionResourcesAvailable, t)
}

func TestPAResAvailableNoOverride(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	// Deployment determined that something's wrong, e.g. the only pod
	// has crashed.
	r.MarkResourcesAvailableFalse("somehow", "somewhere")

	// PodAutoscaler achieved initial scale.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionUnknown,
			}, {
				Type:   av1alpha1.PodAutoscalerConditionScaleTargetInitialized,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	// Verify we did not override this.
	apistest.CheckConditionFailed(r, RevisionConditionResourcesAvailable, t)
	cond := r.GetCondition(RevisionConditionResourcesAvailable)
	if got, notWant := cond.Reason, ReasonProgressDeadlineExceeded; got == notWant {
		t.Error("PA Status propagation overrode the ResourcesAvailable status")
	}
}

func TestPropagateAutoscalerStatusNoProgress(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	// PodAutoscaler is not ready and initial scale was never attained.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		ServiceName: "testRevision",
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}, {
				Type:   av1alpha1.PodAutoscalerConditionScaleTargetInitialized,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	apistest.CheckConditionFailed(r, RevisionConditionActive, t)
	apistest.CheckConditionFailed(r, RevisionConditionResourcesAvailable, t)
	cond := r.GetCondition(RevisionConditionResourcesAvailable)
	if got, want := cond.Reason, ReasonProgressDeadlineExceeded; got != want {
		t.Errorf("Reason = %q, want: %q", got, want)
	}

	// Set a different reason/message
	r.MarkResourcesAvailableFalse("another-one", "bit-the-dust")

	// And apply the status.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}, {
				Type:   av1alpha1.PodAutoscalerConditionScaleTargetInitialized,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	// Verify it did not alter the reason/message.
	cond = r.GetCondition(RevisionConditionResourcesAvailable)
	if got, want := cond.Reason, ReasonProgressDeadlineExceeded; got == want {
		t.Errorf("Reason = %q should have not overridden a different status", got)
	}
}

func TestPropagateAutoscalerStatusRace(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apistest.CheckConditionOngoing(r, RevisionConditionReady, t)

	// PodAutoscaler has no active condition, so we are just coming up.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{},
	})
	apistest.CheckConditionOngoing(r, RevisionConditionActive, t)

	// The PodAutoscaler might have been ready but it's scaled down already.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}, {
				Type:   av1alpha1.PodAutoscalerConditionScaleTargetInitialized,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionFailed(r, RevisionConditionActive, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)
}
