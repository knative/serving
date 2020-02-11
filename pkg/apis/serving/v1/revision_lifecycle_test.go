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
	apitestv1 "knative.dev/pkg/apis/testing/v1"
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

func TestRevisionGetGroupVersionKind(t *testing.T) {
	r := &Revision{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1",
		Kind:    "Revision",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
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
		name:                 "empty status should not be inactive",
		status:               RevisionStatus{},
		isActivationRequired: false,
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
		isActivationRequired: false,
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
		isActivationRequired: false,
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
			if e, a := tc.isActivationRequired, tc.status.IsActivationRequired(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
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
		name:    "empty status should not be ready",
		status:  RevisionStatus{},
		isReady: false,
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
		isReady: false,
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
		isReady: false,
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
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: RevisionStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: RevisionConditionReady,
				}},
			},
		},
		isReady: false,
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

func TestRevisionInitializeConditions(t *testing.T) {
	rs := &RevisionStatus{}
	rs.InitializeConditions()

	var types []string
	for _, cond := range rs.Conditions {
		types = append(types, string(cond.Type))
	}

	expected := []string{
		string(apis.ConditionReady),
		string(RevisionConditionResourcesAvailable),
		string(RevisionConditionContainerHealthy),
	}

	sort.Strings(types)
	sort.Strings(expected)

	if diff := cmp.Diff(expected, types); diff != "" {
		t.Errorf("unexpected conditions %s", diff)
	}
}

func TestTypicalFlowWithProgressDeadlineExceeded(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)

	const want = "the error message"
	r.MarkResourcesAvailableFalse(ReasonProgressDeadlineExceeded, want)
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionReady, t)
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Message != want {
		t.Errorf("MarkProgressDeadlineExceeded = %v, want %v", got, want)
	}
	if got := r.GetCondition(RevisionConditionReady); got == nil || got.Message != want {
		t.Errorf("MarkProgressDeadlineExceeded = %v, want %v", got, want)
	}
}

func TestTypicalFlowWithContainerMissing(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)

	const want = "something about the container being not found"
	r.MarkContainerHealthyFalse(ReasonContainerMissing, want)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionReady, t)
	if got := r.GetCondition(RevisionConditionContainerHealthy); got == nil || got.Message != want {
		t.Errorf("MarkContainerMissing = %v, want %v", got, want)
	} else if got.Reason != "ContainerMissing" {
		t.Errorf("MarkContainerMissing = %v, want %v", got, "ContainerMissing")
	}
	if got := r.GetCondition(RevisionConditionReady); got == nil || got.Message != want {
		t.Errorf("MarkContainerMissing = %v, want %v", got, want)
	} else if got.Reason != "ContainerMissing" {
		t.Errorf("MarkContainerMissing = %v, want %v", got, "ContainerMissing")
	}

	r.MarkContainerHealthyUnknown(ReasonDeploying, want)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)
}

func TestTypicalFlowWithSuspendResume(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)

	// Enter a Ready state.
	r.MarkActiveTrue()
	r.MarkContainerHealthyTrue()
	r.MarkResourcesAvailableTrue()
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionReady, t)

	// From a Ready state, make the revision inactive to simulate scale to zero.
	const want = "Deactivated"
	r.MarkActiveFalse(want, "Reserve")
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionActive, t)
	if got := r.GetCondition(RevisionConditionActive); got == nil || got.Reason != want {
		t.Errorf("MarkInactive = %v, want %v", got, want)
	}
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionReady, t)

	// From an Inactive state, start to activate the revision.
	const want2 = "Activating"
	r.MarkActiveUnknown(want2, "blah blah blah")
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionActive, t)
	if got := r.GetCondition(RevisionConditionActive); got == nil || got.Reason != want2 {
		t.Errorf("MarkInactive = %v, want %v", got, want2)
	}
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionReady, t)

	// From the activating state, simulate the transition back to readiness.
	r.MarkActiveTrue()
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionReady, t)
}

func TestRevisionNotOwnedStuff(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)

	const want = "NotOwned"
	r.MarkResourcesAvailableFalse(ReasonNotOwned, "mark")
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionReady, t)
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %v, want %v", got, want)
	}
	if got := r.GetCondition(RevisionConditionReady); got == nil || got.Reason != want {
		t.Errorf("MarkResourceNotOwned = %v, want %v", got, want)
	}
}

func TestRevisionResourcesUnavailable(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)

	const wantReason, wantMessage = "unschedulable", "insufficient energy"
	r.MarkResourcesAvailableFalse(wantReason, wantMessage)
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionReady, t)
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Reason != wantReason {
		t.Errorf("RevisionConditionResourcesAvailable.Reason = %v, want %v", got, wantReason)
	}
	if got := r.GetCondition(RevisionConditionResourcesAvailable); got == nil || got.Message != wantMessage {
		t.Errorf("RevisionConditionResourcesAvailable.Message = %v, want %v", got, wantMessage)
	}

	r.MarkResourcesAvailableUnknown(wantReason, wantMessage)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)
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
				t.Errorf("got: %#v, want: %#v", got, want)
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
		name:        "Nil annotations",
		annotations: nil,
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
		annotations:       nil,
		setLastPinnedTime: time.Unix(1000, 0),
		expectTime:        time.Unix(1000, 0),
		expectErr:         nil,
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
				t.Errorf("got: %t, want: %t", got, tt.want)
			}
		})
	}
}

func TestPropagateDeploymentStatus(t *testing.T) {
	rev := &RevisionStatus{}
	rev.InitializeConditions()

	// We start out ongoing.
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionResourcesAvailable, t)

	// Empty deployment conditions shouldn't affect our readiness.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{},
	})
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionResourcesAvailable, t)

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
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionContainerHealthy, t)

	// Marking container healthy doesn't affect deployment status.
	rev.MarkContainerHealthyTrue()
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionContainerHealthy, t)

	// We can recover from deployment failures.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionTrue,
		}},
	})
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionContainerHealthy, t)

	// We can go unknown.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionUnknown,
		}},
	})
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionOngoing(rev.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionContainerHealthy, t)

	// ReplicaFailure=True translates into Ready=False.
	rev.PropagateDeploymentStatus(&appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentReplicaFailure,
			Status: corev1.ConditionTrue,
		}},
	})
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionContainerHealthy, t)

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
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionFailed(rev.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionContainerHealthy, t)

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
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionResourcesAvailable, t)
	apitestv1.CheckConditionSucceeded(rev.duck(), RevisionConditionContainerHealthy, t)
}

func TestPropagateAutoscalerStatus(t *testing.T) {
	r := &RevisionStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionReady, t)

	// PodAutoscaler has no active condition, so we are just coming up.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{},
	})
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionActive, t)

	// PodAutoscaler becomes ready, making us active.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionActive, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionReady, t)

	// PodAutoscaler flipping back to Unknown causes Active become ongoing immediately.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	apitestv1.CheckConditionOngoing(r.duck(), RevisionConditionActive, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionReady, t)

	// PodAutoscaler becoming unready makes Active false, but doesn't affect readiness.
	r.PropagateAutoscalerStatus(&av1alpha1.PodAutoscalerStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   av1alpha1.PodAutoscalerConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apitestv1.CheckConditionFailed(r.duck(), RevisionConditionActive, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionReady, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionContainerHealthy, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RevisionConditionResourcesAvailable, t)
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
				t.Errorf("got: %v want: %v", got, want)
			}
		})
	}
}
