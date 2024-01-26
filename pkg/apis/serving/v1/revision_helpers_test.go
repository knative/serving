/*
Copyright 2020 The Knative Authors

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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	net "knative.dev/networking/pkg/apis/networking"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/serving/pkg/apis/serving"
)

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

			if got, want := r.GetProtocol(), tt.protocol; got != want {
				t.Errorf("Protocol = %v, want: %v", got, want)
			}
		})
	}
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
		name: "get serving container info even if there are multiple containers",
		status: RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "firstContainer",
					Image: "firstImage",
					Ports: []corev1.ContainerPort{{
						ContainerPort: 8888,
					}},
				}, {
					Name:  "secondContainer",
					Image: "secondImage",
				}},
			},
		},
		want: &corev1.Container{
			Name:  "firstContainer",
			Image: "firstImage",
			Ports: []corev1.ContainerPort{{
				ContainerPort: 8888,
			}},
		},
	}, {
		name: "get empty container when passed multiple containers without the container port",
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
		want: &corev1.Container{},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if want, got := tc.want, tc.status.GetContainer(); !equality.Semantic.DeepEqual(want, got) {
				t.Errorf("GetContainer: %v want: %v", got, want)
			}
		})
	}
}

func TestSetRoutingState(t *testing.T) {
	rev := &Revision{}
	empty := time.Time{}
	now := time.Now()

	// Test unset timestamp
	if rev.GetRoutingStateModified() != empty {
		t.Error("Expected default value for unset modified annotation.")
	}

	// Test retrieving routing state and modified
	rev.SetRoutingState(RoutingStateActive, now)
	if state := rev.GetRoutingState(); state != RoutingStateActive {
		t.Errorf("Retrieved the wrong state got: %v want: %v", state, RoutingStateActive)
	}

	modified := rev.GetRoutingStateModified()
	if modified == empty {
		t.Error("Expected a non-zero timestamp")
	}

	// Test that no-op modifications don't bump timestamps.
	rev.SetRoutingState(RoutingStateActive, now)
	if rev.GetRoutingStateModified() != modified {
		t.Error("Modified timestamp was bumped, but no change expected.")
	}

	// Test the actual modifications do bump timestamps.
	rev.SetRoutingState(RoutingStateReserve, now)
	if state := rev.GetRoutingState(); state != RoutingStateReserve {
		t.Errorf("Retrieved the wrong state got: %v want: %v", state, RoutingStateReserve)
	}
	if rev.GetRoutingStateModified() != modified {
		t.Error("Expected modified to be bumped.")
	}

	// Test unparsable timestamp.
	rev.Annotations[serving.RoutingStateModifiedAnnotationKey] = "invalid"
	if got, want := rev.GetRoutingStateModified(), empty; got != want {
		t.Error("Expected default value for unparsable annotationm but got:", got)
	}
}
