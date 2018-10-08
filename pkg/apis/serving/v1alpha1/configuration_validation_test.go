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

package v1alpha1

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/pkg/apis"
)

func TestConfigurationSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		c    *ConfigurationSpec
		want *apis.FieldError
	}{{
		name: "valid",
		c: &ConfigurationSpec{
			RevisionTemplate: RevisionTemplateSpec{
				Spec: RevisionSpec{
					Container: corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: nil,
	}, {
		// This is a Configuration specific addition to the basic Revision validation.
		name: "specifies serving state",
		c: &ConfigurationSpec{
			RevisionTemplate: RevisionTemplateSpec{
				Spec: RevisionSpec{
					ServingState: "Active",
					Container: corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("revisionTemplate.spec.servingState"),
	}, {
		name: "propagate revision failures",
		c: &ConfigurationSpec{
			RevisionTemplate: RevisionTemplateSpec{
				Spec: RevisionSpec{
					Container: corev1.Container{
						Name:  "stuart",
						Image: "hellworld",
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("revisionTemplate.spec.container.name"),
	}, {
		name: "build is a BuildSpec",
		c: &ConfigurationSpec{
			Build: &RawExtension{
				BuildSpec: &buildv1alpha1.BuildSpec{
					Steps: []corev1.Container{{
						Image: "foo",
					}},
				},
			},
			RevisionTemplate: RevisionTemplateSpec{
				Spec: RevisionSpec{
					Container: corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: nil,
	}, {
		name: "build is an Object",
		c: &ConfigurationSpec{
			Build: &RawExtension{
				Object: &buildv1alpha1.Build{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "build.knative.dev/v1alpha1",
						Kind:       "Build",
					},
					Spec: buildv1alpha1.BuildSpec{
						Steps: []corev1.Container{{
							Image: "foo",
						}},
					},
				},
			},
			RevisionTemplate: RevisionTemplateSpec{
				Spec: RevisionSpec{
					Container: corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: nil,
	}, {
		name: "build is missing TypeMeta",
		c: &ConfigurationSpec{
			Build: &RawExtension{
				Object: &buildv1alpha1.Build{
					Spec: buildv1alpha1.BuildSpec{
						Steps: []corev1.Container{{
							Image: "foo",
						}},
					},
				},
			},
			RevisionTemplate: RevisionTemplateSpec{
				Spec: RevisionSpec{
					Container: corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: apis.ErrInvalidValue("Object 'Kind' is missing in '{\"metadata\":{\"creationTimestamp\":null},\"spec\":{\"steps\":[{\"name\":\"\",\"image\":\"foo\",\"resources\":{}}]},\"status\":{\"startTime\":null,\"completionTime\":null,\"stepStates\":null,\"stepsCompleted\":null}}'", "build"),
	}, {
		name: "build is not an object",
		c: &ConfigurationSpec{
			Build: &RawExtension{
				Raw: []byte(`"foo"`),
			},
			RevisionTemplate: RevisionTemplateSpec{
				Spec: RevisionSpec{
					Container: corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: apis.ErrInvalidValue("json: cannot unmarshal string into Go value of type map[string]interface {}", "build"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestConfigurationValidation(t *testing.T) {
	tests := []struct {
		name string
		c    *Configuration
		want *apis.FieldError
	}{{
		name: "valid",
		c: &Configuration{
			Spec: ConfigurationSpec{
				RevisionTemplate: RevisionTemplateSpec{
					Spec: RevisionSpec{
						Container: corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "propagate revision failures",
		c: &Configuration{
			Spec: ConfigurationSpec{
				RevisionTemplate: RevisionTemplateSpec{
					Spec: RevisionSpec{
						Container: corev1.Container{
							Name:  "stuart",
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.revisionTemplate.spec.container.name"),
	}, {
		name: "empty spec",
		c:    &Configuration{},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "invalid name - dots",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "do.not.use.dots",
			},
		},
		want: (&apis.FieldError{Message: "Invalid resource name: special character . must not be present", Paths: []string{"metadata.name"}}).
			Also(apis.ErrMissingField("spec")),
	}, {
		name: "invalid name - too long",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("a", 65),
			},
		},
		want: (&apis.FieldError{Message: "Invalid resource name: length must be no more than 63 characters", Paths: []string{"metadata.name"}}).
			Also(apis.ErrMissingField("spec")),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}
