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

	"github.com/knative/pkg/apis"
)

func TestServiceValidation(t *testing.T) {
	tests := []struct {
		name string
		s    *Service
		want *apis.FieldError
	}{{
		name: "valid runLatest",
		s: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "valid pinned",
		s: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid both types",
		s: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
				Pinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got both",
			Paths:   []string{"spec.pinned", "spec.runLatest"},
		},
	}, {
		name: "invalid neither type",
		s:    &Service{},
		want: &apis.FieldError{
			Message: "expected exactly one, got neither",
			Paths:   []string{"spec.pinned", "spec.runLatest"},
		},
	}, {
		name: "invalid runLatest",
		s: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Name:  "foo",
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.runLatest.configuration.revisionTemplate.spec.container.name"),
	}, {
		name: "invalid pinned",
		s: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Name:  "foo",
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.pinned.configuration.revisionTemplate.spec.container.name"),
	}, {
		name: "invalid name - dots",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "do.not.use.dots",
			},
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: &apis.FieldError{Message: "Invalid resource name: special character . must not be present", Paths: []string{"metadata.name"}},
	}, {
		name: "invalid name - too long",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("a", 65),
			},
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: &apis.FieldError{Message: "Invalid resource name: length must be no more than 63 characters", Paths: []string{"metadata.name"}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRunLatestTypeValidation(t *testing.T) {
	tests := []struct {
		name string
		rlt  *RunLatestType
		want *apis.FieldError
	}{{
		name: "valid",
		rlt: &RunLatestType{
			Configuration: ConfigurationSpec{
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
		rlt: &RunLatestType{
			Configuration: ConfigurationSpec{
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
		want: apis.ErrDisallowedFields("configuration.revisionTemplate.spec.container.name"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rlt.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestPinnedTypeValidation(t *testing.T) {
	tests := []struct {
		name string
		pt   *PinnedType
		want *apis.FieldError
	}{{
		name: "valid",
		pt: &PinnedType{
			RevisionName: "foo",
			Configuration: ConfigurationSpec{
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
		name: "missing revision name",
		pt: &PinnedType{
			Configuration: ConfigurationSpec{
				RevisionTemplate: RevisionTemplateSpec{
					Spec: RevisionSpec{
						Container: corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrMissingField("revisionName"),
	}, {
		name: "propagate revision failures",
		pt: &PinnedType{
			RevisionName: "foo",
			Configuration: ConfigurationSpec{
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
		want: apis.ErrDisallowedFields("configuration.revisionTemplate.spec.container.name"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.pt.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}
