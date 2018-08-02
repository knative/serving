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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

var boolTrue = true

func TestBuilds(t *testing.T) {
	tests := []struct {
		name          string
		configuration *v1alpha1.Configuration
		want          *buildv1alpha1.Build
	}{{
		name: "no build",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "no",
				Name:      "build",
			},
			Spec: v1alpha1.ConfigurationSpec{
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "simple build",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "simple",
				Name:      "build",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 31,
				Build: &buildv1alpha1.BuildSpec{
					Steps: []corev1.Container{{
						Image: "busybox",
					}},
				},
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &buildv1alpha1.Build{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "simple",
				Name:      "build-00031",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "build",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: buildv1alpha1.BuildSpec{
				Steps: []corev1.Container{{
					Image: "busybox",
				}},
			},
		},
	}, {
		name: "simple build with template",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "simple",
				Name:      "build-template",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 42,
				Build: &buildv1alpha1.BuildSpec{
					Template: &buildv1alpha1.TemplateInstantiationSpec{
						Name: "buildpacks",
						Arguments: []buildv1alpha1.ArgumentSpec{{
							Name:  "foo",
							Value: "bar",
						}},
					},
				},
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &buildv1alpha1.Build{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "simple",
				Name:      "build-template-00042",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "build-template",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: buildv1alpha1.BuildSpec{
				Template: &buildv1alpha1.TemplateInstantiationSpec{
					Name: "buildpacks",
					Arguments: []buildv1alpha1.ArgumentSpec{{
						Name:  "foo",
						Value: "bar",
					}},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeBuild(test.configuration)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeBuild (-want, +got) = %v", diff)
			}
		})
	}
}
