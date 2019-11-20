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

package v1beta1

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"knative.dev/pkg/apis"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			if !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}

func TestRevisionLabelAnnotationValidation(t *testing.T) {
	validRevisionSpec := v1.RevisionSpec{
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
		name: "invalid knative label",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					"serving.knative.dev/testlabel": "value",
				},
			},
			Spec: validRevisionSpec,
		},
		want: apis.ErrInvalidKeyName("serving.knative.dev/testlabel", "metadata.labels"),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got) = %s", cmp.Diff(want, got))
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
			got := serving.ValidateContainerConcurrency(&test.cc)
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got) = %v", cmp.Diff(want, got))
			}
		})
	}
}

func TestRevisionSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rs   *v1.RevisionSpec
		wc   func(context.Context) context.Context
		want *apis.FieldError
	}{{
		name: "valid",
		rs: &v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
				}},
			},
		},
		want: nil,
	}, {
		name: "with volume (ok)",
		rs: &v1.RevisionSpec{
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
		name: "with volume name collision",
		rs: &v1.RevisionSpec{
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
			Message: fmt.Sprintf(`duplicate volume name "the-name"`),
			Paths:   []string{"name"},
		}).ViaFieldIndex("volumes", 1),
	}, {
		name: "bad pod spec",
		rs: &v1.RevisionSpec{
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
		rs: &v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{},
			},
		},
		want: apis.ErrMissingField("containers"),
	}, {
		name: "too many containers",
		rs: &v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "busybox",
				}, {
					Image: "helloworld",
				}},
			},
		},
		want: apis.ErrMultipleOneOf("containers"),
	}, {
		name: "exceed max timeout",
		rs: &v1.RevisionSpec{
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
		rs: &v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "helloworld",
				}},
			},
			TimeoutSeconds: ptr.Int64(100),
		},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logtesting.TestLogger(t))
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
		rs: &v1.RevisionSpec{
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
				t.Errorf("Validate (-want, +got) = %v", cmp.Diff(want, got))
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceName("cpu"): resource.MustParse("50m"),
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
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceName("cpu"): resource.MustParse("100m"),
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
			Spec: v1.RevisionSpec{
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
		rts  *v1.RevisionTemplateSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rts: &v1.RevisionTemplateSpec{
			Spec: v1.RevisionSpec{
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
		rts:  &v1.RevisionTemplateSpec{},
		want: apis.ErrMissingField("spec.containers"),
	}, {
		name: "nested spec error",
		rts: &v1.RevisionTemplateSpec{
			Spec: v1.RevisionSpec{
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
		rts: &v1.RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				// We let users bring their own revision name.
				Name: "parent-foo",
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid metadata.annotations for scale",
		rts: &v1.RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					autoscaling.MinScaleAnnotationKey: "5",
					autoscaling.MaxScaleAnnotationKey: "",
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: (&apis.FieldError{
			Message: "expected 1 <=  <= 2147483647",
			Paths:   []string{autoscaling.MaxScaleAnnotationKey},
		}).ViaField("annotations").ViaField("metadata"),
	}, {
		name: "Queue sidecar resource percentage annotation more than 100",
		rts: &v1.RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "200",
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
		},
		want: (&apis.FieldError{
			Message: "expected 0.1 <= 200 <= 100",
			Paths:   []string{serving.QueueSideCarResourcePercentageAnnotation},
		}).ViaField("metadata.annotations"),
	}, {
		name: "Invalid queue sidecar resource percentage annotation",
		rts: &v1.RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "50mx",
				},
			},
			Spec: v1.RevisionSpec{
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
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := apis.WithinParent(context.Background(), metav1.ObjectMeta{
				Name: "parent",
			})

			got := test.rts.Validate(ctx)
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got) = %v", cmp.Diff(want, got))
			}
		})
	}
}
