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

	caching "knative.dev/caching/pkg/apis/caching/v1alpha1"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestMakeImageCache(t *testing.T) {
	tests := []struct {
		name          string
		rev           *v1.Revision
		containerName string
		image         string
		want          *caching.Image
	}{{
		name: "simple container",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				Annotations: map[string]string{
					"a":                                     "b",
					serving.RevisionLastPinnedAnnotationKey: "c",
				},
				UID: "1234",
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
			Status: v1.RevisionStatus{
				ContainerStatuses: []v1.ContainerStatus{{
					Name:        "user-container",
					ImageDigest: "busybox@sha256:deadbeef",
				}},
				DeprecatedImageDigest: "busybox@sha256:deadbeef",
			},
		},
		containerName: "user-container",
		image:         "busybox@sha256:deadbeef",
		want: &caching.Image{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-cache-user-container",
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
			Spec: caching.ImageSpec{
				Image: "busybox@sha256:deadbeef",
			},
		},
	}, {
		name: "multiple containers",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				Annotations: map[string]string{
					"a":                                     "b",
					serving.RevisionLastPinnedAnnotationKey: "c",
				},
				UID: "1234",
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "ubuntu",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 80,
						}},
					}, {
						Image: "busybox",
					}},
				},
			},
			Status: v1.RevisionStatus{
				ContainerStatuses: []v1.ContainerStatus{{
					Name:        "user-container1",
					ImageDigest: "ubuntu@sha256:deadbeef1",
				}, {
					Name:        "user-container",
					ImageDigest: "busybox@sha256:deadbeef",
				}},
				DeprecatedImageDigest: "busybox@sha256:deadbeef",
			},
		},
		containerName: "user-container1",
		image:         "ubuntu@sha256:deadbeef1",
		want: &caching.Image{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-cache-user-container1",
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
			Spec: caching.ImageSpec{
				Image: "ubuntu@sha256:deadbeef1",
			},
		},
	}, {
		name: "Image cache for multiple containers when image digests are empty",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				Annotations: map[string]string{
					"a":                                     "b",
					serving.RevisionLastPinnedAnnotationKey: "c",
				},
				UID: "1234",
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "user-container",
						Image: "ubuntu",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 80,
						}},
					}, {
						Name:  "user-container1",
						Image: "busybox",
					}},
				},
			},
			Status: v1.RevisionStatus{
				ContainerStatuses: []v1.ContainerStatus{{}},
			},
		},
		containerName: "user-container",
		image:         "",
		want: &caching.Image{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-cache-user-container",
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
			Spec: caching.ImageSpec{
				Image: "",
			},
		},
	}, {
		name: "with service account",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				PodSpec: corev1.PodSpec{
					ServiceAccountName: "privilegeless",
					Containers: []corev1.Container{{
						Name:  "user-container",
						Image: "busybox",
					}},
				},
			},
			Status: v1.RevisionStatus{
				ContainerStatuses: []v1.ContainerStatus{{
					Name:        "user-container",
					ImageDigest: "busybox@sha256:deadbeef",
				}},
			},
		},
		containerName: "user-container",
		image:         "busybox@sha256:deadbeef",
		want: &caching.Image{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-cache-user-container",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: caching.ImageSpec{
				Image:              "busybox@sha256:deadbeef",
				ServiceAccountName: "privilegeless",
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeImageCache(test.rev, test.containerName, test.image)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Error("MakeImageCache (-want, +got) =", diff)
			}
		})
	}
}
