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
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"

	"knative.dev/pkg/apis"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRevisionValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *Revision
		want *apis.FieldError
	}{{
		name: "valid",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid container concurrency",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
				ContainerConcurrency: ptr.Int64(-10),
			},
		},
		want: apis.ErrOutOfBoundsValue(
			-10, 0, config.DefaultMaxRevisionContainerConcurrency,
			"spec.containerConcurrency"),
	}}

	// TODO(dangerd): PodSpec validation failures.

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got): \n%s", cmp.Diff(want, got))
			}
		})
	}
}

func TestRevisionLabelAnnotationValidation(t *testing.T) {
	validRevisionSpec := RevisionSpec{
		PodSpec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
		},
	}
	tests := []struct {
		name string
		r    *Revision
		want *apis.FieldError
	}{{
		name: "valid route name",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.RouteLabelKey: "test-route",
				},
			},
			Spec: validRevisionSpec,
		},
		want: nil,
	}, {
		name: "valid knative service name",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
			},
			Spec: validRevisionSpec,
		},
		want: nil,
	}, {
		name: "valid knative service name",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ConfigurationGenerationLabelKey: "1234",
				},
			},
			Spec: validRevisionSpec,
		},
		want: nil,
	}, {
		name: "invalid knative configuration name",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "absent-cfg",
				},
			},
			Spec: validRevisionSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/configuration"),
	}, {
		name: "valid knative configuration name",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "test-cfg",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Configuration",
					Name:       "test-cfg",
				}},
			},
			Spec: validRevisionSpec,
		},
		want: nil,
	}, {
		name: "invalid knative configuration name without owner ref",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Configuration",
					Name:       "diff-cfg",
				}},
			},
			Spec: validRevisionSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/configuration"),
	}, {
		name: "invalid knative configuration name with multiple owner ref",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "test-cfg",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "NewConfiguration",
					Name:       "test-new-cfg",
				}, {
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Configuration",
					Name:       "test-cfg",
				}},
			},
			Spec: validRevisionSpec,
		},
	}, {
		name: "Mismatch knative configuration label and owner ref",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ConfigurationLabelKey: "test-cfg",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "BrandNewService",
					Name:       "brand-new-svc",
				}},
			},
			Spec: validRevisionSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/configuration"),
	}, {
		// We want to be able to introduce new labels with the serving prefix in the future
		// and not break downgrading.
		name: "allow unknown uses of knative.dev/serving prefix",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					"serving.knative.dev/testlabel": "value",
				},
			},
			Spec: validRevisionSpec,
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got): \n%s", cmp.Diff(want, got))
			}
		})
	}
}

func TestContainerConcurrencyValidation(t *testing.T) {
	tests := []struct {
		name string
		cc   int64
		want *apis.FieldError
	}{{
		name: "single",
		cc:   1,
		want: nil,
	}, {
		name: "unlimited",
		cc:   0,
		want: nil,
	}, {
		name: "ten",
		cc:   10,
		want: nil,
	}, {
		name: "invalid container concurrency (too small)",
		cc:   -1,
		want: apis.ErrOutOfBoundsValue(-1, 0, config.DefaultMaxRevisionContainerConcurrency,
			apis.CurrentField),
	}, {
		name: "invalid container concurrency (too large)",
		cc:   config.DefaultMaxRevisionContainerConcurrency + 1,
		want: apis.ErrOutOfBoundsValue(config.DefaultMaxRevisionContainerConcurrency+1,
			0, config.DefaultMaxRevisionContainerConcurrency, apis.CurrentField),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := serving.ValidateContainerConcurrency(context.Background(), &test.cc)
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got): \n%s", cmp.Diff(want, got))
			}
		})
	}
}

func TestRevisionSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rs   *RevisionSpec
		wc   func(context.Context) context.Context
		want *apis.FieldError
	}{{
		name: "valid",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
				}},
			},
		},
		want: nil,
	}, {
		name: "with volume (ok)",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
					VolumeMounts: []corev1.VolumeMount{{
						MountPath: "/mount/path",
						Name:      "the-name",
						ReadOnly:  true,
					}},
				}},
				Volumes: []corev1.Volume{{
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo",
						},
					},
				}},
			},
		},
		want: nil,
	}, {
		name: "with multi containers (ok)",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "busybox",
					Ports: []corev1.ContainerPort{{
						ContainerPort: 8881,
					}},
				}, {
					Image: "helloworld",
				}},
			},
		},
		want: nil,
	}, {
		name: "with volume name collision",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
					VolumeMounts: []corev1.VolumeMount{{
						MountPath: "/mount/path",
						Name:      "the-name",
						ReadOnly:  true,
					}},
				}},
				Volumes: []corev1.Volume{{
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo",
						},
					},
				}, {
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{},
					},
				}},
			},
		},
		want: (&apis.FieldError{
			Message: `duplicate volume name "the-name"`,
			Paths:   []string{"name"},
		}).ViaFieldIndex("volumes", 1),
	}, {
		name: "bad pod spec",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:      "steve",
					Image:     "helloworld",
					Lifecycle: &corev1.Lifecycle{},
				}},
			},
		},
		want: apis.ErrDisallowedFields("containers[0].lifecycle"),
	}, {
		name: "missing container",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{},
			},
		},
		want: apis.ErrMissingField("containers"),
	}, {
		name: "too many containers",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "busybox",
				}, {
					Image: "helloworld",
				}},
			},
		},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logtesting.TestLogger(t))
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.DefaultsConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.FeaturesConfigName,
				},
				Data: map[string]string{
					"multi-container": "disabled",
				},
			})
			return s.ToContext(ctx)
		},
		want: &apis.FieldError{Message: "multi-container is off, but found 2 containers"},
	}, {
		name: "exceed max timeout",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
				}},
			},
			TimeoutSeconds: ptr.Int64(6000),
		},
		want: apis.ErrOutOfBoundsValue(
			6000, 0, config.DefaultMaxRevisionTimeoutSeconds,
			"timeoutSeconds"),
	}, {
		name: "exceed custom max timeout",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
				}},
			},
			TimeoutSeconds: ptr.Int64(100),
		},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logtesting.TestLogger(t))
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"revision-timeout-seconds":     "25",
					"max-revision-timeout-seconds": "50"},
			})
			return s.ToContext(ctx)
		},
		want: apis.ErrOutOfBoundsValue(100, 0, 50, "timeoutSeconds"),
	}, {
		name: "negative timeout",
		rs: &RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
				}},
			},
			TimeoutSeconds: ptr.Int64(-30),
		},
		want: apis.ErrOutOfBoundsValue(
			-30, 0, config.DefaultMaxRevisionTimeoutSeconds,
			"timeoutSeconds"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got := test.rs.Validate(ctx)
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got): \n%s", cmp.Diff(want, got))
			}
		})
	}
}

func TestImmutableFields(t *testing.T) {
	tests := []struct {
		name string
		new  *Revision
		old  *Revision
		wc   func(context.Context) context.Context
		want *apis.FieldError
	}{{
		name: "good (no change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: nil,
	}, {
		// Test the case where max-revision-timeout is changed to a value
		// that is less than an existing revision's timeout value.
		// Existing revision should keep operating normally.
		name: "good (max revision timeout change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
				TimeoutSeconds: ptr.Int64(100),
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
				TimeoutSeconds: ptr.Int64(100),
			},
		},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logtesting.TestLogger(t))
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"revision-timeout-seconds":     "25",
					"max-revision-timeout-seconds": "50"},
			})
			return s.ToContext(ctx)
		},
		want: nil,
	}, {
		name: "bad (resources image change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					}},
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("100m"),
							},
						},
					}},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1.RevisionSpec}.PodSpec.Containers[0].Resources.Requests["cpu"]:
	-: resource.Quantity: "{i:{value:100 scale:-3} d:{Dec:<nil>} s:100m Format:DecimalSI}"
	+: resource.Quantity: "{i:{value:50 scale:-3} d:{Dec:<nil>} s:50m Format:DecimalSI}"
`,
		},
	}, {
		name: "bad (container image change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1.RevisionSpec}.PodSpec.Containers[0].Image:
	-: "busybox"
	+: "helloworld"
`,
		},
	}, {
		name: "bad (concurrency model change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
				ContainerConcurrency: ptr.Int64(1),
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
				ContainerConcurrency: ptr.Int64(2),
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `*{v1.RevisionSpec}.ContainerConcurrency:
	-: "2"
	+: "1"
`,
		},
	}, {
		name: "bad (new field added)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
					ServiceAccountName: "foobar",
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1.RevisionSpec}.PodSpec.ServiceAccountName:
	-: ""
	+: "foobar"
`,
		},
	}, {
		name: "bad (multiple changes)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					ServiceAccountName: "foobar",
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1.RevisionSpec}.PodSpec.Containers[0].Image:
	-: "busybox"
	+: "helloworld"
{v1.RevisionSpec}.PodSpec.ServiceAccountName:
	-: ""
	+: "foobar"
`,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := apis.WithinUpdate(context.Background(), test.old)
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got := test.new.Validate(ctx)
			if got, want := got.Error(), test.want.Error(); got != want {
				t.Errorf("Validate got: %s, want: %s, diff:(-want, +got)=\n%v", got, want, cmp.Diff(got, want))
			}
		})
	}
}

func TestRevisionTemplateSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		rts  *RevisionTemplateSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rts: &RevisionTemplateSpec{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "empty spec",
		rts:  &RevisionTemplateSpec{},
		want: apis.ErrMissingField("spec.containers"),
	}, {
		name: "nested spec error",
		rts: &RevisionTemplateSpec{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      "kevin",
						Image:     "helloworld",
						Lifecycle: &corev1.Lifecycle{},
					}},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.containers[0].lifecycle"),
	}, {
		name: "has revision template name",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				// We let users bring their own revision name.
				Name: "parent-foo",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "valid name for revision template",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				// When user provides empty string in the name field it will behave like no name provided.
				Name: "",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid name for revision template",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				// We let users bring their own revision name.
				Name: "parent-@foo-bar",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: apis.ErrInvalidValue("not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"metadata.name"),
	}, {
		name: "invalid generate name for revision template",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				// We let users bring their own revision generate name.
				GenerateName: "parent-@foo-bar",
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: apis.ErrInvalidValue("not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"metadata.generateName"),
	}, {
		name: "invalid metadata.annotations for scale",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					autoscaling.MinScaleAnnotationKey: "5",
					autoscaling.MaxScaleAnnotationKey: "covid-19",
				},
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: apis.ErrInvalidValue("covid-19", autoscaling.MaxScaleAnnotationKey).
			ViaField("annotations").ViaField("metadata"),
	}, {
		name: "Queue sidecar resource percentage annotation more than 100",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "200",
				},
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: (&apis.FieldError{
			Message: "expected 0.1 <= 200 <= 100",
			Paths:   []string{"[" + serving.QueueSideCarResourcePercentageAnnotation + "]"},
		}).ViaField("metadata.annotations"),
	}, {
		name: "Invalid queue sidecar resource percentage annotation",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "50mx",
				},
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: (&apis.FieldError{
			Message: "invalid value: 50mx",
			Paths:   []string{fmt.Sprintf("[%s]", serving.QueueSideCarResourcePercentageAnnotation)},
		}).ViaField("metadata.annotations"),
	}, {
		name: "Invalid initial scale when cluster doesn't allow zero",
		ctx:  autoscalerConfigCtx(false, 1),
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					autoscaling.InitialScaleAnnotationKey: "0",
				},
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: (&apis.FieldError{
			Message: "invalid value: 0",
			Paths:   []string{autoscaling.InitialScaleAnnotationKey},
		}).ViaField("metadata.annotations"),
	}, {
		name: "Valid initial scale when cluster allows zero",
		ctx:  autoscalerConfigCtx(true, 1),
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					autoscaling.InitialScaleAnnotationKey: "0",
				},
			},
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.ctx != nil {
				ctx = test.ctx
			}
			ctx = apis.WithinParent(ctx, metav1.ObjectMeta{
				Name: "parent",
			})

			got := test.rts.Validate(ctx)
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got):\n%s", cmp.Diff(want, got))
			}
		})
	}
}

func autoscalerConfigCtx(allowInitialScaleZero bool, initialScale int) context.Context {
	testConfigs := &config.Config{}
	testConfigs.Autoscaler, _ = autoscalerconfig.NewConfigFromMap(map[string]string{
		"allow-zero-initial-scale": strconv.FormatBool(allowInitialScaleZero),
		"initial-scale":            strconv.Itoa(initialScale),
	})
	return config.ToContext(context.Background(), testConfigs)
}

func TestValidateQueueSidecarAnnotation(t *testing.T) {
	cases := []struct {
		name       string
		annotation map[string]string
		expectErr  *apis.FieldError
	}{{
		name: "too small",
		annotation: map[string]string{
			serving.QueueSideCarResourcePercentageAnnotation: "0.01982",
		},
		expectErr: &apis.FieldError{
			Message: "expected 0.1 <= 0.01982 <= 100",
			Paths:   []string{fmt.Sprintf("[%s]", serving.QueueSideCarResourcePercentageAnnotation)},
		},
	}, {
		name: "too big for Queue sidecar resource percentage annotation",
		annotation: map[string]string{
			serving.QueueSideCarResourcePercentageAnnotation: "100.0001",
		},
		expectErr: &apis.FieldError{
			Message: "expected 0.1 <= 100.0001 <= 100",
			Paths:   []string{fmt.Sprintf("[%s]", serving.QueueSideCarResourcePercentageAnnotation)},
		},
	}, {
		name: "Invalid queue sidecar resource percentage annotation",
		annotation: map[string]string{
			serving.QueueSideCarResourcePercentageAnnotation: "",
		},
		expectErr: &apis.FieldError{
			Message: "invalid value: ",
			Paths:   []string{fmt.Sprintf("[%s]", serving.QueueSideCarResourcePercentageAnnotation)},
		},
	}, {
		name:       "empty annotation",
		annotation: map[string]string{},
	}, {
		name: "different annotation other than QueueSideCarResourcePercentageAnnotation",
		annotation: map[string]string{
			serving.CreatorAnnotation: "umph",
		},
	}, {
		name: "valid value for Queue sidecar resource percentage annotation",
		annotation: map[string]string{
			serving.QueueSideCarResourcePercentageAnnotation: "0.1",
		},
	}, {
		name: "valid value for Queue sidecar resource percentage annotation",
		annotation: map[string]string{
			serving.QueueSideCarResourcePercentageAnnotation: "100",
		},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateQueueSidecarAnnotation(c.annotation)
			if got, want := err.Error(), c.expectErr.Error(); got != want {
				t.Errorf("Got: %q want: %q", got, want)
			}
		})
	}
}

func TestValidateTimeoutSecond(t *testing.T) {
	cases := []struct {
		name      string
		timeout   *int64
		expectErr *apis.FieldError
	}{{
		name:    "exceed max timeout",
		timeout: ptr.Int64(6000),
		expectErr: apis.ErrOutOfBoundsValue(
			6000, 0, config.DefaultMaxRevisionTimeoutSeconds,
			"timeoutSeconds"),
	}, {
		name:    "valid timeout value",
		timeout: ptr.Int64(100),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateTimeoutSeconds(context.Background(), *c.timeout)
			if got, want := err.Error(), c.expectErr.Error(); got != want {
				t.Errorf("Got: %q want: %q", got, want)
			}
		})
	}
}

func TestValidateRevisionName(t *testing.T) {
	cases := []struct {
		name            string
		revName         string
		revGenerateName string
		objectMeta      metav1.ObjectMeta
		expectErr       *apis.FieldError
	}{{
		name:            "invalid revision generateName - dots",
		revGenerateName: "foo.bar",
		expectErr: apis.ErrInvalidValue("not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"metadata.generateName"),
	}, {
		name:    "invalid revision name - dots",
		revName: "foo.bar",
		expectErr: apis.ErrInvalidValue("not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"metadata.name"),
	}, {
		name: "invalid name (not prefixed)",
		objectMeta: metav1.ObjectMeta{
			Name: "bar",
		},
		revName: "foo",
		expectErr: apis.ErrInvalidValue(`"foo" must have prefix "bar-"`,
			"metadata.name"),
	}, {
		name: "invalid name (with generateName)",
		objectMeta: metav1.ObjectMeta{
			GenerateName: "foo-bar-",
		},
		revName:   "foo-bar-foo",
		expectErr: apis.ErrDisallowedFields("metadata.name"),
	}, {
		name: "valid name",
		objectMeta: metav1.ObjectMeta{
			Name: "valid",
		},
		revName: "valid-name",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := apis.WithinParent(context.Background(), c.objectMeta)
			err := validateRevisionName(ctx, c.revName, c.revGenerateName)
			if got, want := err.Error(), c.expectErr.Error(); got != want {
				t.Errorf("Got: %q want: %q", got, want)
			}
		})
	}
}
