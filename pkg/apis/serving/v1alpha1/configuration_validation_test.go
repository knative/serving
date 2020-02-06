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

	"knative.dev/pkg/ptr"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
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
					RevisionSpec: v1.RevisionSpec{
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
					RevisionSpec: v1.RevisionSpec{
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
			if diff := cmp.Diff(test.want.Error(), test.c.Validate(context.Background()).Error()); diff != "" {
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
		name: "invalid name",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
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
		want: &apis.FieldError{
			Message: "name or generateName is required",
			Paths:   []string{"metadata.name"},
		},
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
	}, {
		name: "invalid name for configuration spec",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo@bar",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue("not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"spec.revisionTemplate.metadata.name"),
	}, {
		name: "invalid generate name for configuration spec",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "foo@bar",
					},
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue("not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"spec.revisionTemplate.metadata.generateName"),
	}, {
		name: "valid generate name for configuration spec",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-generatename",
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
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.want.Error(), test.c.Validate(context.Background()).Error()); diff != "" {
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
			Paths:   []string{"spec.revisionTemplate.metadata.name"},
			Details: "{*v1alpha1.RevisionTemplateSpec}.Spec.DeprecatedContainer.Image:\n\t-: \"helloworld:bar\"\n\t+: \"helloworld:foo\"\n",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.old)
			if diff := cmp.Diff(test.want.Error(), test.new.Validate(ctx).Error()); diff != "" {
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
						RevisionSpec: v1.RevisionSpec{
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
						RevisionSpec: v1.RevisionSpec{
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
						RevisionSpec: v1.RevisionSpec{
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
						RevisionSpec: v1.RevisionSpec{
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
			if diff := cmp.Diff(test.want.Error(), test.config.Validate(ctx).Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func getConfigurationSpec(image string) ConfigurationSpec {
	return ConfigurationSpec{
		Template: &RevisionTemplateSpec{
			Spec: RevisionSpec{
				RevisionSpec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{Containers: []corev1.Container{{
						Image: image,
					}},
					},
					TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds),
				},
			},
		},
	}
}

func TestConfigurationAnnotationUpdate(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	tests := []struct {
		name string
		prev *Configuration
		this *Configuration
		want *apis.FieldError
	}{{
		name: "update creator annotation",
		this: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		prev: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		want: &apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{"metadata.annotations." + serving.CreatorAnnotation}},
	}, {
		name: "update creator annotation with spec changes",
		this: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:bar"),
		},
		prev: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		want: &apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{"metadata.annotations." + serving.CreatorAnnotation}},
	}, {
		name: "update lastModifier without spec changes",
		this: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u2,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		prev: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		want: apis.ErrInvalidValue(u2, "metadata.annotations."+serving.UpdaterAnnotation),
	}, {
		name: "update lastModifier with spec changes",
		this: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
			},
			Spec: getConfigurationSpec("helloworld:bar"),
		},
		prev: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		want: nil,
	}, {
		name: "no validation for lastModifier annotation even after update as configuration owned by service",
		this: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1alpha1",
					Kind:       serving.GroupName,
				}},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		prev: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		want: nil,
	}, {
		name: "no validation for creator annotation even after update as configuration owned by service",
		this: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u3,
					serving.UpdaterAnnotation: u1,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1alpha1",
					Kind:       serving.GroupName,
				}},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		prev: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getConfigurationSpec("helloworld:foo"),
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.prev)
			if diff := cmp.Diff(test.want.Error(), test.this.Validate(ctx).Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
