/*
Copyright 2019 The Knative Authors

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

package v1

import (
	"context"
	"testing"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid container concurrency",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						ContainerConcurrency: ptr.Int64(-10),
					},
				},
			},
		},
		want: apis.ErrOutOfBoundsValue(
			-10, 0, config.DefaultMaxRevisionContainerConcurrency,
			"spec.template.spec.containerConcurrency"),
	}, {
		name: "valid BYO name",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
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
		name: "invalid name",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
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
		name: "valid BYO name (with generateName)",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "byo-name-",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.template.metadata.name"),
	}, {
		name: "invalid BYO name (not prefixed)",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue(`"foo" must have prefix "byo-name-"`,
			"spec.template.metadata.name"),
	}, {
		name: "invalid name for configuration spec",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo.bar",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue("not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"spec.template.metadata.name"),
	}, {
		name: "invalid generate name for configuration spec",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "foo.bar",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue("not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"spec.template.metadata.generateName"),
	}, {
		name: "valid generate name for configuration spec",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-generatename",
					},
					Spec: RevisionSpec{
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
		name: "invalid autoscaling.knative.dev annotation",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "autoscaling-annotation",
				Annotations: map[string]string{
					"autoscaling.knative.dev/foo": "bar",
				},
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "hellworld",
							}},
						},
					},
				},
			},
		},
		want: apis.ErrInvalidKeyName("autoscaling.knative.dev/foo", "metadata.annotations", `autoscaling annotations must be put under "spec.template.metadata.annotations" to work`),
	}}

	// TODO(dangerd): PodSpec validation failures.

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate(context.Background())
			if !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}

func TestConfigurationLabelValidation(t *testing.T) {
	validConfigSpec := ConfigurationSpec{
		Template: RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name-foo",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "hellworld",
					}},
				},
			},
		},
	}
	tests := []struct {
		name string
		c    *Configuration
		want *apis.FieldError
	}{{
		name: "valid route name",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.RouteLabelKey: "test-route",
				},
			},
			Spec: validConfigSpec,
		},
		want: nil,
	}, {
		name: "valid knative service name",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Service",
					Name:       "test-svc",
				}},
			},
			Spec: validConfigSpec,
		},
		want: nil,
	}, {
		name: "invalid knative service name without matching owner references",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Service",
					Name:       "absent-svc",
				}},
			},
			Spec: validConfigSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/service"),
	}, {
		name: "invalid knative service name with multiple owner ref",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "NewSerice",
					Name:       "test-new-svc",
				}, {
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Service",
					Name:       "test-svc",
				}},
			},
			Spec: validConfigSpec,
		},
	}, {
		name: "invalid knative service name",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "absent-svc",
				},
			},
			Spec: validConfigSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/service"),
	}, {
		name: "Mismatch knative service label and owner ref",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "BrandNewService",
					Name:       "brand-new-svc",
				}},
			},
			Spec: validConfigSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/service"),
	}, {
		// We want to be able to introduce new labels with the serving prefix in the future
		// and not break downgrading.
		name: "allow unknown uses of knative.dev/serving prefix",
		c: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					"serving.knative.dev/testlabel": "value",
				},
			},
			Spec: validConfigSpec,
		},
		want: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Validate(context.Background())
			if !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:foo",
							}},
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:bar",
							}},
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
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:foo",
							}},
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
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-bar",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:bar",
							}},
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
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:foo",
							}},
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
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:foo",
							}},
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
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:foo",
							}},
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
				Template: RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-name-foo",
					},
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "helloworld:bar",
							}},
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Saw the following changes without a name change (-old +new)",
			Paths:   []string{"spec.template.metadata.name"},
			Details: "{*v1.RevisionTemplateSpec}.Spec.PodSpec.Containers[0].Image:\n\t-: \"helloworld:bar\"\n\t+: \"helloworld:foo\"\n",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.old)
			got := test.new.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v\nwant: %v\ngot: %v",
					diff, test.want, got)
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
					},
				},
			},
		},
		subresource: "foo",
		want: apis.ErrOutOfBoundsValue(config.DefaultMaxRevisionTimeoutSeconds+1, 0,
			config.DefaultMaxRevisionTimeoutSeconds,
			"spec.template.spec.timeoutSeconds"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinSubResourceUpdate(ctx, test.config, test.subresource)
			got := test.config.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Error("Validate (-want, +got) =", diff)
			}
		})
	}
}

func getConfigurationSpec(image string) ConfigurationSpec {
	return ConfigurationSpec{
		Template: RevisionTemplateSpec{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: image,
					}},
				},
				TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds),
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
		want: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
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
		want: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier annotation without spec changes",
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
		want: apis.ErrInvalidValue(u2, serving.UpdaterAnnotation).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier annotation with spec changes",
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
		name: "no validation for lastModifier annotation even after update without spec changes as configuration owned by service",
		this: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1",
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
					APIVersion: "v1",
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
				t.Error("Validate (-want, +got) =", diff)
			}
		})
	}
}
