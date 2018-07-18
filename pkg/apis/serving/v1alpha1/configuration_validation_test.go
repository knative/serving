/*
Copyright 2017 The Knative Authors

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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
)

func TestConfigurationSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		c    *ConfigurationSpec
		want *FieldError
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
		want: errDisallowedFields("revisionTemplate.spec.servingState"),
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
		want: errDisallowedFields("revisionTemplate.spec.container.name"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestConfigurationValidation(t *testing.T) {
	tests := []struct {
		name string
		c    *Configuration
		want *FieldError
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
		want: errDisallowedFields("spec.revisionTemplate.spec.container.name"),
	}, {
		name: "empty spec",
		c:    &Configuration{},
		want: errMissingField("spec"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}
