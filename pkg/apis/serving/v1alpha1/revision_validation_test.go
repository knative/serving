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
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/config"
	net "knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestConcurrencyModelValidation(t *testing.T) {
	tests := []struct {
		name string
		cm   DeprecatedRevisionRequestConcurrencyModelType
		want *apis.FieldError
	}{{
		name: "single",
		cm:   DeprecatedRevisionRequestConcurrencyModelSingle,
		want: nil,
	}, {
		name: "multi",
		cm:   DeprecatedRevisionRequestConcurrencyModelMulti,
		want: nil,
	}, {
		name: "empty",
		cm:   "",
		want: nil,
	}, {
		name: "bogus",
		cm:   "bogus",
		want: apis.ErrInvalidValue("bogus", apis.CurrentField),
	}, {
		name: "balderdash",
		cm:   "balderdash",
		want: apis.ErrInvalidValue("balderdash", apis.CurrentField),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.cm.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
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
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
		},
		want: nil,
	}, {
		name: "invalid deprecated fields",
		wc:   apis.DisallowDeprecated,
		rs: &RevisionSpec{
			DeprecatedGeneration:   123,
			DeprecatedServingState: "Active",
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
			DeprecatedConcurrencyModel: "Multi",
			DeprecatedBuildName:        "banana",
		},
		want: apis.ErrDisallowedFields("buildName", "concurrencyModel", "container",
			"generation", "servingState"),
	}, {
		name: "missing container",
		rs: &RevisionSpec{
			RevisionSpec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
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
		},
		want: apis.ErrMissingOneOf("container", "containers"),
	}, {
		name: "more container",
		rs: &RevisionSpec{
			RevisionSpec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
			},
			DeprecatedContainer: &corev1.Container{
				Name: "deprecatedContainer",
			},
		},
		want: apis.ErrMultipleOneOf("container", "containers"),
	}, {
		name: "with ContainerConcurrency",
		rs: &RevisionSpec{
			RevisionSpec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "helloworld",
					}},
				},
				ContainerConcurrency: ptr.Int64(10),
			},
		},
		want: nil,
	}, {
		name: "with volume (ok)",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
			RevisionSpec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
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
		},
		want: nil,
	}, {
		name: "with volume name collision",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
			RevisionSpec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
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
		},
		want: (&apis.FieldError{
			Message: fmt.Sprintf(`duplicate volume name "the-name"`),
			Paths:   []string{"name"},
		}).ViaFieldIndex("volumes", 1),
	}, {
		name: "has build ref (disallowed)",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
			DeprecatedBuildRef: &corev1.ObjectReference{},
		},
		want: apis.ErrDisallowedFields("buildRef"),
	}, {
		name: "bad concurrency model",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
			DeprecatedConcurrencyModel: "bogus",
		},
		want: apis.ErrInvalidValue("bogus", "concurrencyModel"),
	}, {
		name: "bad container spec",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Name:      "steve",
				Image:     "helloworld",
				Lifecycle: &corev1.Lifecycle{},
			},
		},
		want: apis.ErrDisallowedFields("container.lifecycle"),
	}, {
		name: "exceed max timeout",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
			RevisionSpec: v1.RevisionSpec{
				TimeoutSeconds: ptr.Int64(6000),
			},
		},
		want: apis.ErrOutOfBoundsValue(6000, 0,
			config.DefaultMaxRevisionTimeoutSeconds,
			"timeoutSeconds"),
	}, {
		name: "exceed custom max timeout",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
			RevisionSpec: v1.RevisionSpec{
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
		want: apis.ErrOutOfBoundsValue(100, 0, 50, "timeoutSeconds"),
	}, {
		name: "provided zero timeout (ok)",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
			RevisionSpec: v1.RevisionSpec{
				TimeoutSeconds: ptr.Int64(0),
			},
		},
		want: nil,
	}, {
		name: "negative timeout",
		rs: &RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "helloworld",
			},
			RevisionSpec: v1.RevisionSpec{
				TimeoutSeconds: ptr.Int64(-30),
			},
		},
		want: apis.ErrOutOfBoundsValue(-30, 0,
			config.DefaultMaxRevisionTimeoutSeconds,
			"timeoutSeconds"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got := test.rs.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRevisionTemplateSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rts  *RevisionTemplateSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rts: &RevisionTemplateSpec{
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: nil,
	}, {
		name: "empty spec",
		rts:  &RevisionTemplateSpec{},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "nested spec error",
		rts: &RevisionTemplateSpec{
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Name:      "kevin",
					Image:     "helloworld",
					Lifecycle: &corev1.Lifecycle{},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.container.lifecycle"),
	}, {
		name: "has revision template name",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				// We let users bring their own revision name.
				Name: "parent-foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: nil,
	}, {
		name: "Queue sidecar resource percentage annotation more than 100",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "200",
				},
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: (&apis.FieldError{
			Message: "expected 0.1 <= 200 <= 100",
			Paths:   []string{serving.QueueSideCarResourcePercentageAnnotation},
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
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: (&apis.FieldError{
			Message: "invalid value: 50mx",
			Paths:   []string{fmt.Sprintf("[%s]", serving.QueueSideCarResourcePercentageAnnotation)},
		}).ViaField("metadata.annotations"),
	}, {
		name: "invalid metadata.annotations for scale",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					autoscaling.MinScaleAnnotationKey: "5",
					autoscaling.MaxScaleAnnotationKey: "",
				},
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: (&apis.FieldError{
			Message: "expected 1 <=  <= 2147483647",
			Paths:   []string{autoscaling.MaxScaleAnnotationKey},
		}).ViaField("annotations").ViaField("metadata"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := apis.WithinParent(context.Background(), metav1.ObjectMeta{
				Name: "parent",
			})

			got := test.rts.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

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
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: nil,
	}, {
		name: "empty spec",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
		},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "nested spec error",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Name:      "kevin",
					Image:     "helloworld",
					Lifecycle: &corev1.Lifecycle{},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.container.lifecycle"),
	}, {
		name: "invalid name - dots and too long",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "a" + strings.Repeat(".", 62) + "a",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: &apis.FieldError{
			Message: "not a DNS 1035 label: [must be no more than 63 characters a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "invalid metadata.annotations - scale bounds",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "scale-bounds",
				Annotations: map[string]string{
					autoscaling.MinScaleAnnotationKey: "5",
					autoscaling.MaxScaleAnnotationKey: "2",
				},
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: (&apis.FieldError{
			Message: "maxScale=2 is less than minScale=5",
			Paths:   []string{autoscaling.MaxScaleAnnotationKey, autoscaling.MinScaleAnnotationKey},
		}).ViaField("annotations").ViaField("metadata"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if got, want := got.Error(), test.want.Error(); got != want {
				t.Errorf("Validate got:\n%s, want:\n%s, diff:(-want, +got)=\n%v", got, want, cmp.Diff(got, want))
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
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
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
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
				RevisionSpec: v1.RevisionSpec{
					TimeoutSeconds: ptr.Int64(100),
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
				RevisionSpec: v1.RevisionSpec{
					TimeoutSeconds: ptr.Int64(100),
				},
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
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("cpu"): resource.MustParse("50m"),
						},
					},
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("cpu"): resource.MustParse("100m"),
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.DeprecatedContainer.Resources.Requests["cpu"]:
	-: resource.Quantity: "{i:{value:100 scale:-3} d:{Dec:<nil>} s:100m Format:DecimalSI}"
	+: resource.Quantity: "{i:{value:50 scale:-3} d:{Dec:<nil>} s:50m Format:DecimalSI}"
`,
		},
	}, {
		name: "bad (container image change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "busybox",
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.DeprecatedContainer.Image:
	-: "busybox"
	+: "helloworld"
`,
		},
	}, {
		name: "bad (concurrency model change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Single",
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.DeprecatedConcurrencyModel:
	-: "Single"
	+: "Multi"
`,
		},
	}, {
		name: "bad (new field added)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
				RevisionSpec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{
						ServiceAccountName: "foobar",
					},
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.RevisionSpec.PodSpec.ServiceAccountName:
	-: ""
	+: "foobar"
`,
		},
	}, {
		name: "bad (multiple changes)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "helloworld",
				},
				RevisionSpec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{
						ServiceAccountName: "foobar",
					},
				},
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "busybox",
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.RevisionSpec.PodSpec.ServiceAccountName:
	-: ""
	+: "foobar"
{v1alpha1.RevisionSpec}.DeprecatedContainer.Image:
	-: "busybox"
	+: "helloworld"
`,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.old)
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got := test.new.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRevisionProtocolType(t *testing.T) {
	tests := []struct {
		p    net.ProtocolType
		want *apis.FieldError
	}{{
		net.ProtocolH2C, nil,
	}, {
		net.ProtocolHTTP1, nil,
	}, {
		net.ProtocolType(""), apis.ErrMissingField(apis.CurrentField),
	}, {
		net.ProtocolType("token-ring"), apis.ErrInvalidValue("token-ring", apis.CurrentField),
	}}
	for _, test := range tests {
		e := test.p.Validate(context.Background())
		if got, want := e.Error(), test.want.Error(); !cmp.Equal(got, want) {
			t.Errorf("Got = %v, want: %v, diff: %s", got, want, cmp.Diff(got, want))
		}
	}
}
