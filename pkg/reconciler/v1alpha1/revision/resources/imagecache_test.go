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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	caching "github.com/knative/caching/pkg/apis/caching/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestMakeImageCache(t *testing.T) {
	tests := []struct {
		name    string
		rev     *v1alpha1.Revision
		deploy  *appsv1.Deployment
		want    *caching.Image
		wantErr bool
	}{{
		name: "simple container",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
		deploy: &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  userContainerName,
							Image: "busybox",
						}},
					},
				},
			},
		},
		want: &caching.Image{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-cache",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: caching.ImageSpec{
				Image: "busybox",
			},
		},
	}, {
		name: "with service account",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				ServiceAccountName:   "privilegeless",
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
		deploy: &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "privilegeless",
						Containers: []corev1.Container{{
							Name:  userContainerName,
							Image: "busybox",
						}},
					},
				},
			},
		},
		want: &caching.Image{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-cache",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: caching.ImageSpec{
				Image:              "busybox",
				ServiceAccountName: "privilegeless",
			},
		},
	}, {
		name: "no user container",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				ServiceAccountName:   "privilegeless",
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
		deploy: &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "privilegeless",
						Containers: []corev1.Container{{
							Name:  "wrong-name",
							Image: "busybox",
						}},
					},
				},
			},
		},
		wantErr: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MakeImageCache(test.rev, test.deploy)
			if (err != nil) != test.wantErr {
				t.Errorf("MakeImageCache() = (%v, %v), wantErr: %v",
					got, err, test.wantErr)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeImageCache (-want, +got) = %v", diff)
			}
		})
	}
}
