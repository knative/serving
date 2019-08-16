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
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/ptr"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

func TestMakePA(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		want *av1alpha1.PodAutoscaler
	}{{
		name: "name is bar (Concurrency=1)",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Annotations: map[string]string{
					"a":                                     "b",
					serving.RevisionLastPinnedAnnotationKey: "timeless",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: ptr.Int64(1),
				},
			},
		},
		want: &av1alpha1.PodAutoscaler{
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
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: av1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 1,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar-deployment",
				},
				ProtocolType: networking.ProtocolHTTP1,
			},
		},
	}, {
		name: "name is baz (Concurrency=0)",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "baz",
				UID:       "4321",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: ptr.Int64(0),
				},
				DeprecatedContainer: &corev1.Container{
					Ports: []corev1.ContainerPort{{
						Name:     "h2c",
						HostPort: int32(443),
					}},
				},
			},
		},
		want: &av1alpha1.PodAutoscaler{
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
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "baz",
					UID:                "4321",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: av1alpha1.PodAutoscalerSpec{
				ContainerConcurrency: 0,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "baz-deployment",
				},
				ProtocolType: networking.ProtocolH2C,
			}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakePA(test.rev)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("MakeK8sService (-want, +got) = %v", diff)
			}
		})
	}
}
