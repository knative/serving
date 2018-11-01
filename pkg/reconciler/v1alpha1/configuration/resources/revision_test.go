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

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestMakeRevisions(t *testing.T) {
	tests := []struct {
		name          string
		configuration *v1alpha1.Configuration
		want          *v1alpha1.Revision
	}{{
		name: "no build",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "no",
				Name:      "build",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 12,
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "no",
				Name:      "build-00012",
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "build",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "build",
					serving.ConfigurationGenerationLabelKey: "12",
					serving.ServiceLabelKey:       "",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
	}, {
		name: "with build",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "build",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 99,
				Build: &v1alpha1.RawExtension{BuildSpec: &buildv1alpha1.BuildSpec{
					Steps: []corev1.Container{{
						Image: "busybox",
					}},
				}},
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "build-00099",
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "build",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "build",
					serving.ConfigurationGenerationLabelKey: "99",
					serving.ServiceLabelKey:       "",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				BuildName: "build-00099",
				BuildRef: &corev1.ObjectReference{
					APIVersion: "build.knative.dev/v1alpha1",
					Kind:       "Build",
					Name:       "build-00099",
				},
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
	}, {
		name: "with labels",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "labels",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 99,
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"foo": "bar",
							"baz": "blah",
						},
					},
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "labels-00099",
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "labels",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "labels",
					serving.ConfigurationGenerationLabelKey: "99",
					serving.ServiceLabelKey:       "",

					"foo": "bar",
					"baz": "blah",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
	}, {
		name: "with annotations",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "annotations",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 99,
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"foo": "bar",
							"baz": "blah",
						},
					},
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "annotations-00099",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "annotations",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "annotations",
					serving.ConfigurationGenerationLabelKey: "99",
					serving.ServiceLabelKey:       "",
				},
				Annotations: map[string]string{
					"foo": "bar",
					"baz": "blah",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeRevision(test.configuration)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeRevision (-want, +got) = %v", diff)
			}
		})
	}
}
