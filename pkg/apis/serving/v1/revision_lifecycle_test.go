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
	"time"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apistest "knative.dev/pkg/apis/testing"
	"knative.dev/pkg/ptr"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/config"
	net "knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
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

func TestIsActivationRequired(t *testing.T) {
	cases := []struct {
		name                 string
		status               RevisionStatus
		isActivationRequired bool
	}{{
		name:   "empty status should not be inactive",
		status: RevisionStatus{},
	}, {
		name: "Ready status should not be inactive",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	}, {
		name: "Inactive status should be inactive",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionActive,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isActivationRequired: true,
	}, {
		name: "Updating status should be inactive",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionUnknown,
					Reason: "Updating",
				}, {
					Type:   RevisionConditionActive,
					Status: corev1.ConditionUnknown,
					Reason: "Updating",
				}},
			},
		},
		isActivationRequired: true,
	}, {
		name: "NotReady status without reason should not be inactive",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
	}, {
		name: "Ready/Unknown status without reason should not be inactive",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RevisionConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isActivationRequired: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.status.IsActivationRequired(), tc.isActivationRequired; got != want {
				t.Errorf("IsActivationRequired = %v, want: %v", got, want)
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

func TestRevisionGetProtocol(t *testing.T) {
	containerWithPortName := func(name string) corev1.Container {
		return corev1.Container{Ports: []corev1.ContainerPort{{Name: name}}}
	}

	tests := []struct {
		name      string
		container corev1.Container
		protocol  net.ProtocolType
	}{{
		name:      "undefined",
		container: corev1.Container{},
		protocol:  net.ProtocolHTTP1,
	}, {
		name:      "http1",
		container: containerWithPortName("http1"),
		protocol:  net.ProtocolHTTP1,
	}, {
		name:      "h2c",
		container: containerWithPortName("h2c"),
		protocol:  net.ProtocolH2C,
	}, {
		name:      "unknown",
		container: containerWithPortName("whatever"),
		protocol:  net.ProtocolHTTP1,
	}, {
		name:      "empty",
		container: containerWithPortName(""),
		protocol:  net.ProtocolHTTP1,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Revision{
				Spec: RevisionSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{
							tt.container,
						},
					},
				},
			}

			got := r.GetProtocol()
			want := tt.protocol

			if got != want {
				t.Errorf("Protocol = %v, want: %v", got, want)
			}
		})
	}
}

func TestRevisionGetLastPinned(t *testing.T) {
	cases := []struct {
		name              string
		annotations       map[string]string
		expectTime        time.Time
		setLastPinnedTime time.Time
		expectErr         error
	}{{
		name: "Nil annotations",
		expectErr: LastPinnedParseError{
			Type: AnnotationParseErrorTypeMissing,
		},
	}, {
		name:        "Empty map annotations",
		annotations: map[string]string{},
		expectErr: LastPinnedParseError{
			Type: AnnotationParseErrorTypeMissing,
		},
	}, {
		name:              "Empty map annotations - with set time",
		annotations:       map[string]string{},
		setLastPinnedTime: time.Unix(1000, 0),
		expectTime:        time.Unix(1000, 0),
	}, {
		name:        "Invalid time",
		annotations: map[string]string{serving.RevisionLastPinnedAnnotationKey: "abcd"},
		expectErr: LastPinnedParseError{
			Type:  AnnotationParseErrorTypeInvalid,
			Value: "abcd",
		},
	}, {
		name:        "Valid time",
		annotations: map[string]string{serving.RevisionLastPinnedAnnotationKey: "10000"},
		expectTime:  time.Unix(10000, 0),
	}, {
		name:              "Valid time empty annotations",
		setLastPinnedTime: time.Unix(1000, 0),
		expectTime:        time.Unix(1000, 0),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev := Revision{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tc.annotations,
				},
			}

			if tc.setLastPinnedTime != (time.Time{}) {
				rev.SetLastPinned(tc.setLastPinnedTime)
			}

			pt, err := rev.GetLastPinned()
			failErr := func() {
				t.Fatalf("Expected error %v got %v", tc.expectErr, err)
			}

			if tc.expectErr == nil {
				if err != nil {
					failErr()
				}
			} else {
				if tc.expectErr.Error() != err.Error() {
					failErr()
				}
			}

			if tc.expectTime != pt {
				t.Fatalf("Expected pin time %v got %v", tc.expectTime, pt)
			}
		})
	}
}

func TestRevisionIsReachable(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{{
		name:   "has route annotation",
		labels: map[string]string{serving.RouteLabelKey: "the-route"},
		want:   true,
	}, {
		name:   "empty route annotation",
		labels: map[string]string{serving.RouteLabelKey: ""},
		want:   false,
	}, {
		name:   "no route annotation",
		labels: nil,
		want:   false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rev := Revision{ObjectMeta: metav1.ObjectMeta{Labels: tt.labels}}

			got := rev.IsReachable()

			if got != tt.want {
				t.Errorf("IsReachable = %t, want: %t", got, tt.want)
			}
		})
	}
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
			}},
		},
	})
	apistest.CheckConditionFailed(r, RevisionConditionActive, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionReady, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionContainerHealthy, t)
	apistest.CheckConditionSucceeded(r, RevisionConditionResourcesAvailable, t)
}

func TestGetContainer(t *testing.T) {
	cases := []struct {
		name   string
		status RevisionSpec
		want   *corev1.Container
	}{{
		name:   "empty revisionSpec should return default value",
		status: RevisionSpec{},
		want:   &corev1.Container{},
	}, {
		name: "get deprecatedContainer info",
		status: RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "deprecatedContainer",
					Image: "foo",
				}},
			},
		},
		want: &corev1.Container{
			Name:  "deprecatedContainer",
			Image: "foo",
		},
	}, {
		name: "get first container info even after passing multiple",
		status: RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "firstContainer",
					Image: "firstImage",
				}, {
					Name:  "secondContainer",
					Image: "secondImage",
				}},
			},
		},
		want: &corev1.Container{
			Name:  "firstContainer",
			Image: "firstImage",
		},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if want, got := tc.want, tc.status.GetContainer(); !equality.Semantic.DeepEqual(want, got) {
				t.Errorf("GetContainer: %v want: %v", got, want)
			}
		})
	}
}
