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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"

	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/ptr"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestMakePA(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		want *autoscalingv1alpha1.PodAutoscaler
	}{{
		name: "name is bar (Concurrency=1, Reachable=true)",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					serving.RoutingStateLabelKey: string(v1.RoutingStateActive),
				},
				Annotations: map[string]string{
					"a": "b",
				},
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
			},
		},
		want: &autoscalingv1alpha1.PodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{
					"a": "b",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: autoscalingv1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 1,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar-deployment",
				},
				ProtocolType: networking.ProtocolHTTP1,
				Reachability: autoscalingv1alpha1.ReachabilityReachable,
			},
		},
	}, {
		name: "name is baz (Concurrency=0, Reachable=false)",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "baz",
				UID:       "4321",
				Labels: map[string]string{
					serving.RoutingStateLabelKey: string(v1.RoutingStateReserve),
				},
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Ports: []corev1.ContainerPort{{
							Name:     "h2c",
							HostPort: int32(443),
						}},
					}},
				},
			},
		},
		want: &autoscalingv1alpha1.PodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "baz",
				Labels: map[string]string{
					serving.RevisionLabelKey: "baz",
					serving.RevisionUID:      "4321",
					AppLabelKey:              "baz",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "baz",
					UID:                "4321",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: autoscalingv1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 0,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "baz-deployment",
				},
				ProtocolType: networking.ProtocolH2C,
				Reachability: autoscalingv1alpha1.ReachabilityUnreachable,
			}},
	}, {
		name: "unknown routing state",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "batman",
				UID:       "4321",
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Ports: []corev1.ContainerPort{{
							Name:     "h2c",
							HostPort: int32(443),
						}},
					}},
				},
			},
		},
		want: &autoscalingv1alpha1.PodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "batman",
				Labels: map[string]string{
					serving.RevisionLabelKey: "batman",
					serving.RevisionUID:      "4321",
					AppLabelKey:              "batman",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "batman",
					UID:                "4321",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: autoscalingv1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 0,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "batman-deployment",
				},
				ProtocolType: networking.ProtocolH2C,
				// When the Revision has failed, we mark the PA as unreachable.
				Reachability: autoscalingv1alpha1.ReachabilityUnknown,
			}},
	}, {
		name: "pending routing state",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "batman",
				UID:       "4321",
				Labels: map[string]string{
					serving.RoutingStateLabelKey: string(v1.RoutingStatePending),
				},
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Ports: []corev1.ContainerPort{{
							Name:     "h2c",
							HostPort: int32(443),
						}},
					}},
				},
			},
		},
		want: &autoscalingv1alpha1.PodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "batman",
				Labels: map[string]string{
					serving.RevisionLabelKey: "batman",
					serving.RevisionUID:      "4321",
					AppLabelKey:              "batman",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "batman",
					UID:                "4321",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: autoscalingv1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 0,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "batman-deployment",
				},
				ProtocolType: networking.ProtocolH2C,
				// When the Revision has failed, we mark the PA as unreachable.
				Reachability: autoscalingv1alpha1.ReachabilityUnknown,
			}},
	}, {
		name: "failed deployment",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "batman",
				UID:       "4321",
				Labels: map[string]string{
					serving.RoutingStateLabelKey: string(v1.RoutingStateActive),
				},
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Ports: []corev1.ContainerPort{{
							Name:     "h2c",
							HostPort: int32(443),
						}},
					}},
				},
			},
			Status: v1.RevisionStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{{
						Type:   v1.RevisionConditionResourcesAvailable,
						Status: corev1.ConditionFalse,
					}},
				},
			},
		},
		want: &autoscalingv1alpha1.PodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "blah",
				Name:        "batman",
				Annotations: map[string]string{},
				Labels: map[string]string{
					serving.RevisionLabelKey: "batman",
					serving.RevisionUID:      "4321",
					AppLabelKey:              "batman",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "batman",
					UID:                "4321",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: autoscalingv1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 0,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "batman-deployment",
				},
				ProtocolType: networking.ProtocolH2C,
				Reachability: autoscalingv1alpha1.ReachabilityUnreachable,
			},
		},
	}, {
		// Crashlooping container that never starts
		name: "failed container",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "batman",
				UID:       "4321",
				Labels: map[string]string{
					serving.RoutingStateLabelKey: string(v1.RoutingStateActive),
				},
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Ports: []corev1.ContainerPort{{
							Name:     "h2c",
							HostPort: int32(443),
						}},
					}},
				},
			},
			Status: v1.RevisionStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{{
						Type:   v1.RevisionConditionContainerHealthy,
						Status: corev1.ConditionFalse,
					}},
				},
			},
		},
		want: &autoscalingv1alpha1.PodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "blah",
				Name:        "batman",
				Annotations: map[string]string{},
				Labels: map[string]string{
					serving.RevisionLabelKey: "batman",
					serving.RevisionUID:      "4321",
					AppLabelKey:              "batman",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "batman",
					UID:                "4321",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: autoscalingv1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 0,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "batman-deployment",
				},
				ProtocolType: networking.ProtocolH2C,
				Reachability: autoscalingv1alpha1.ReachabilityUnreachable,
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakePA(test.rev)
			if !cmp.Equal(got, test.want) {
				t.Error("MakePA (-want, +got) =", cmp.Diff(test.want, got))
			}
		})
	}
}
