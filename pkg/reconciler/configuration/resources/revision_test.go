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
	"github.com/knative/pkg/ptr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
				Namespace:  "no",
				Name:       "build",
				Generation: 10,
			},
			Spec: v1alpha1.ConfigurationSpec{
				DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    "no",
				GenerateName: "build-",
				Annotations:  map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "build",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "build",
					serving.ConfigurationGenerationLabelKey: "10",
					serving.ServiceLabelKey:                 "",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "busybox",
				},
			},
		},
	}, {
		name: "with labels",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  "with",
				Name:       "labels",
				Generation: 100,
			},
			Spec: v1alpha1.ConfigurationSpec{
				DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"foo": "bar",
							"baz": "blah",
						},
					},
					Spec: v1alpha1.RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    "with",
				GenerateName: "labels-",
				Annotations:  map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "labels",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "labels",
					serving.ConfigurationGenerationLabelKey: "100",
					serving.ServiceLabelKey:                 "",
					"foo":                                   "bar",
					"baz":                                   "blah",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "busybox",
				},
			},
		},
	}, {
		name: "with annotations",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  "with",
				Name:       "annotations",
				Generation: 100,
			},
			Spec: v1alpha1.ConfigurationSpec{
				DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"foo": "bar",
							"baz": "blah",
						},
					},
					Spec: v1alpha1.RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    "with",
				GenerateName: "annotations-",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "annotations",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "annotations",
					serving.ConfigurationGenerationLabelKey: "100",
					serving.ServiceLabelKey:                 "",
				},
				Annotations: map[string]string{
					"foo": "bar",
					"baz": "blah",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				DeprecatedContainer: &corev1.Container{
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

		t.Run(test.name+"(template)", func(t *testing.T) {
			// Test the Template variant.
			test.configuration.Spec.Template = test.configuration.Spec.DeprecatedRevisionTemplate
			test.configuration.Spec.DeprecatedRevisionTemplate = nil
			got := MakeRevision(test.configuration)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeRevision (-want, +got) = %v", diff)
			}
		})
	}
}
