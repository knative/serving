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
	"context"
	"strings"
	"testing"

	"github.com/knative/pkg/ptr"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/config"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
)

func TestConfigurationSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		c    *ConfigurationSpec
		want *apis.FieldError
	}{{
		name: "valid",
		c: &ConfigurationSpec{
			DeprecatedRevisionTemplate: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					DeprecatedContainer: &corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: nil,
	}, {
		name: "valid podspec",
		c: &ConfigurationSpec{
			DeprecatedRevisionTemplate: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					RevisionSpec: v1beta1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "propagate revision failures",
		c: &ConfigurationSpec{
			DeprecatedRevisionTemplate: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					DeprecatedContainer: &corev1.Container{
						Name:      "stuart",
						Image:     "hellworld",
						Lifecycle: &corev1.Lifecycle{},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("revisionTemplate.spec.container.lifecycle"),
	}, {
		name: "build is not allowed",
		c: &ConfigurationSpec{
			DeprecatedBuild: &runtime.RawExtension{},
			DeprecatedRevisionTemplate: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					DeprecatedContainer: &corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("build"),
	}, {
		name: "no revision template",
		c: &ConfigurationSpec{
			DeprecatedBuild: &runtime.RawExtension{},
		},
		want: apis.ErrMissingOneOf("revisionTemplate", "template"),
	}, {
		name: "too many revision templates",
		c: &ConfigurationSpec{
			DeprecatedRevisionTemplate: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					DeprecatedContainer: &corev1.Container{
						Image: "hellworld",
					},
				},
			},
			Template: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					DeprecatedContainer: &corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: apis.ErrMultipleOneOf("revisionTemplate", "template"),
	}, {
		name: "just template",
		c: &ConfigurationSpec{
			Template: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					RevisionSpec: v1beta1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "just template (don't allow deprecated fields)",
		c: &ConfigurationSpec{
			Template: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					DeprecatedConcurrencyModel: "Multi",
					DeprecatedContainer: &corev1.Container{
						Image: "hellworld",
					},
				},
			},
		},
		want: apis.ErrDisallowedFields(
			"template.spec.concurrencyModel", "template.spec.container"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate(context.Background())
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
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
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
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Name:      "stuart",
							Image:     "hellworld",
							Lifecycle: &corev1.Lifecycle{},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.revisionTemplate.spec.container.lifecycle"),
	}, {
		name: "propagate revision failures (template)",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				Template: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.template.spec.container"),
	}, {
		name: "empty spec",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
		},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "invalid name - dots",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "do.not.use.dots",
			},
		},
		want: (&apis.FieldError{
			Message: "not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"metadata.name"},
		}).Also(apis.ErrMissingField("spec")),
	}, {
		name: "invalid name - too long",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("a", 64),
			},
		},
		want: (&apis.FieldError{
			Message: "not a DNS 1035 label: [must be no more than 63 characters]",
			Paths:   []string{"metadata.name"},
		}).Also(apis.ErrMissingField("spec")),
	}, {
		name: "valid BYO name",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid BYO name (with generateName)",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "byo-name-",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.revisionTemplate.metadata.name"),
	}, {
		name: "invalid BYO name (not prefixed)",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue(`"foo" must have prefix "byo-name-"`,
			"spec.revisionTemplate.metadata.name"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestImmutableConfigurationFields(t *testing.T) {
	tests := []struct {
		name string
		new  *Configuration
		old  *Configuration
		want *apis.FieldError
	}{{
		name: "without byo-name",
		new: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:foo",
						},
					},
				},
			},
		},
		old: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:bar",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo name change",
		new: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:foo",
						},
					},
				},
			},
		},
		old: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-bar",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:bar",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo name (no change)",
		new: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:foo",
						},
					},
				},
			},
		},
		old: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:foo",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "bad byo name change",
		new: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:foo",
						},
					},
				},
			},
		},
		old: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld:bar",
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Saw the following changes without a name change (-old +new)",
			Paths:   []string{"spec.revisionTemplate"},
			Details: "{*v1alpha1.RevisionTemplateSpec}.Spec.DeprecatedContainer.Image:\n\t-: \"helloworld:bar\"\n\t+: \"helloworld:foo\"\n",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.old)
			got := test.new.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestConfigurationSubresourceUpdate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Configuration
		subresource string
		want        *apis.FieldError
	}{{
		name: "status update with valid revision template",
		config: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld",
						},
						RevisionSpec: v1beta1.RevisionSpec{
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
						},
					},
				},
			},
		},
		subresource: "status",
		want:        nil,
	}, {
		name: "status update with invalid revision template",
		config: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld",
						},
						RevisionSpec: v1beta1.RevisionSpec{
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
						},
					},
				},
			},
		},
		subresource: "status",
		want:        nil,
	}, {
		name: "non-status sub resource update with valid revision template",
		config: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld",
						},
						RevisionSpec: v1beta1.RevisionSpec{
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
						},
					},
				},
			},
		},
		subresource: "foo",
		want:        nil,
	}, {
		name: "non-status sub resource update with invalid revision template",
		config: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "helloworld",
						},
						RevisionSpec: v1beta1.RevisionSpec{
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
						},
					},
				},
			},
		},
		subresource: "foo",
		want: apis.ErrOutOfBoundsValue(config.DefaultMaxRevisionTimeoutSeconds+1, 0,
			config.DefaultMaxRevisionTimeoutSeconds,
			"spec.revisionTemplate.spec.timeoutSeconds"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinSubResourceUpdate(ctx, test.config, test.subresource)
			got := test.config.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
