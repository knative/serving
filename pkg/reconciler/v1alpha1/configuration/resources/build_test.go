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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

var boolTrue = true

func TestBuilds(t *testing.T) {
	tests := []struct {
		name          string
		configuration *v1alpha1.Configuration
		want          *unstructured.Unstructured
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
		name: "nil build",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "no",
				Name:      "build",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Build: UnstructuredWithContent(nil),
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
				Build: UnstructuredWithContent(map[string]interface{}{
					"steps": []interface{}{map[string]interface{}{
						"image": "busybox",
					}},
				}),
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: UnstructuredWithContent(map[string]interface{}{
			"apiVersion": "build.knative.dev/v1alpha1",
			"kind":       "Build",
			"metadata": map[string]interface{}{
				"namespace": "simple",
				"name":      "build-00031",
				"ownerReferences": []interface{}{map[string]interface{}{
					"apiVersion":         v1alpha1.SchemeGroupVersion.String(),
					"kind":               "Configuration",
					"name":               "build",
					"uid":                "",
					"controller":         true,
					"blockOwnerDeletion": true,
				}},
			},
			"spec": map[string]interface{}{
				"steps": []interface{}{map[string]interface{}{
					"image": "busybox",
				}},
			},
		}),
	}, {
		name: "simple build with type meta",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "simple",
				Name:      "build",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 31,
				Build: UnstructuredWithContent(map[string]interface{}{
					"apiVersion": "foo.knative.dev/v1alfoo1",
					"kind":       "Foo",
					"steps": []interface{}{map[string]interface{}{
						"image": "busybox",
					}},
				}),
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: UnstructuredWithContent(map[string]interface{}{
			"apiVersion": "foo.knative.dev/v1alfoo1",
			"kind":       "Foo",
			"metadata": map[string]interface{}{
				"namespace": "simple",
				"name":      "build-00031",
				"ownerReferences": []interface{}{map[string]interface{}{
					"apiVersion":         v1alpha1.SchemeGroupVersion.String(),
					"kind":               "Configuration",
					"name":               "build",
					"uid":                "",
					"controller":         true,
					"blockOwnerDeletion": true,
				}},
			},
			"spec": map[string]interface{}{
				"steps": []interface{}{map[string]interface{}{
					"image": "busybox",
				}},
			},
		}),
	}, {
		name: "simple build with template",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "simple",
				Name:      "build-template",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 42,
				Build: UnstructuredWithContent(map[string]interface{}{
					"template": map[string]interface{}{
						"name": "buildpacks",
						"arguments": []interface{}{map[string]interface{}{
							"name":  "foo",
							"value": "bar",
						}},
					},
				}),
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: UnstructuredWithContent(map[string]interface{}{
			"apiVersion": "build.knative.dev/v1alpha1",
			"kind":       "Build",
			"metadata": map[string]interface{}{
				"namespace": "simple",
				"name":      "build-template-00042",
				"ownerReferences": []interface{}{map[string]interface{}{
					"apiVersion":         v1alpha1.SchemeGroupVersion.String(),
					"kind":               "Configuration",
					"name":               "build-template",
					"uid":                "",
					"controller":         true,
					"blockOwnerDeletion": true,
				}},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"name": "buildpacks",
					"arguments": []interface{}{map[string]interface{}{
						"name":  "foo",
						"value": "bar",
					}},
				},
			},
		}),
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
