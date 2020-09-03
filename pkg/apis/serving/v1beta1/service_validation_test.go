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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	network "knative.dev/networking/pkg"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	"knative.dev/pkg/apis"
)

func TestServiceValidation(t *testing.T) {
	goodConfigSpec := v1.ConfigurationSpec{
		Template: v1.RevisionTemplateSpec{
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
	}
	goodRouteSpec := v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			LatestRevision: ptr.Bool(true),
			Percent:        ptr.Int64(100),
		}},
	}

	tests := []struct {
		name string
		r    *Service
		want *apis.FieldError
	}{{
		name: "valid run latest",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "valid visibility label",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Labels: map[string]string{
					network.VisibilityLabelKey: "cluster-local",
				},
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec:         goodRouteSpec,
			},
		},
		want: nil,
	}, {
		name: "invalid knative label",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Labels: map[string]string{
					"serving.knative.dev/name": "some-value",
				},
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec:         goodRouteSpec,
			},
		},
		want: apis.ErrInvalidKeyName("serving.knative.dev/name", "metadata.labels"),
	}, {
		name: "valid non knative label",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Labels: map[string]string{
					"serving.name": "some-name",
				},
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec:         goodRouteSpec,
			},
		},
		want: nil,
	}, {
		name: "invalid visibility label value",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Labels: map[string]string{
					network.VisibilityLabelKey: "bad-label",
				},
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec:         goodRouteSpec,
			},
		},
		want: apis.ErrInvalidValue("bad-label", "metadata.labels."+network.VisibilityLabelKey),
	}, {
		name: "valid release",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						Tag:            "current",
						LatestRevision: ptr.Bool(false),
						RevisionName:   "valid-00001",
						Percent:        ptr.Int64(98),
					}, {
						Tag:            "candidate",
						LatestRevision: ptr.Bool(false),
						RevisionName:   "valid-00002",
						Percent:        ptr.Int64(2),
					}, {
						Tag:            "latest",
						LatestRevision: ptr.Bool(true),
						Percent:        nil,
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid configurationName",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						ConfigurationName: "valid",
						LatestRevision:    ptr.Bool(true),
						Percent:           ptr.Int64(100),
					}},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.traffic[0].configurationName"),
	}, {
		name: "invalid latestRevision",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						RevisionName:   "valid",
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want: apis.ErrGeneric(`may not set revisionName "valid" when latestRevision is true`, "spec.traffic[0].latestRevision"),
	}, {
		name: "invalid container concurrency",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "busybox",
								}},
							},
							ContainerConcurrency: ptr.Int64(-10),
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want: apis.ErrOutOfBoundsValue(
			-10, 0, config.DefaultMaxRevisionContainerConcurrency,
			"spec.template.spec.containerConcurrency"),
	}}

	// TODO(dangerd): PodSpec validation failures.
	// TODO(mattmoor): BYO revision name.

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

func TestImmutableServiceFields(t *testing.T) {
	tests := []struct {
		name string
		new  *Service
		old  *Service
		want *apis.FieldError
	}{{
		name: "without byo-name",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:bar",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo-name (name change)",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "byo-name-foo",
						},
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						RevisionName: "byo-name-foo", // Used it!
						Percent:      ptr.Int64(100),
					}},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "byo-name-bar",
						},
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:bar",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						RevisionName: "byo-name-bar", // Used it!
						Percent:      ptr.Int64(100),
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo-name (with delta)",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "byo-name-foo",
						},
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						RevisionName: "byo-name-bar", // Leave old.
						Percent:      ptr.Int64(100),
					}},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "byo-name-bar",
						},
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:bar",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						RevisionName: "byo-name-bar", // Used it!
						Percent:      ptr.Int64(100),
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "bad byo-name",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "byo-name-foo",
						},
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "byo-name-foo",
						},
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:bar",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
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

func TestServiceSubresourceUpdate(t *testing.T) {
	tests := []struct {
		name        string
		service     *Service
		subresource string
		want        *apis.FieldError
	}{{
		name: "status update with valid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		subresource: "status",
		want:        nil,
	}, {
		name: "status update with invalid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		subresource: "status",
		want:        nil,
	}, {
		name: "status update with invalid status",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
			Status: v1.ServiceStatus{
				RouteStatusFields: v1.RouteStatusFields{
					Traffic: []v1.TrafficTarget{{
						Tag:          "bar",
						RevisionName: "foo",
						Percent:      ptr.Int64(50), URL: &apis.URL{
							Scheme: "http",
							Host:   "foo.bar.com",
						},
					}},
				},
			},
		},
		subresource: "status",
		want: &apis.FieldError{
			Message: "Traffic targets sum to 50, want 100",
			Paths:   []string{"status.traffic"},
		},
	}, {
		name: "non-status sub resource update with valid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		subresource: "foo",
		want:        nil,
	}, {
		name: "non-status sub resource update with invalid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld:foo",
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
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
			ctx = apis.WithinUpdate(ctx, test.service)
			ctx = apis.WithinSubResourceUpdate(ctx, test.service, test.subresource)
			if diff := cmp.Diff(test.want.Error(), test.service.Validate(ctx).Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func getServiceSpec(image string) v1.ServiceSpec {
	return v1.ServiceSpec{
		ConfigurationSpec: v1.ConfigurationSpec{
			Template: v1.RevisionTemplateSpec{
				Spec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Image: image,
						}},
					},
					TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds),
				},
			},
		},
		RouteSpec: v1.RouteSpec{
			Traffic: []v1.TrafficTarget{{
				LatestRevision: ptr.Bool(true),
				Percent:        ptr.Int64(100),
			}},
		},
	}
}

func TestServiceAnnotationUpdate(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	tests := []struct {
		name string
		prev *Service
		this *Service
		want *apis.FieldError
	}{{
		name: "update creator annotation",
		this: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		prev: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		want: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier without spec changes",
		this: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u2,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		prev: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		want: apis.ErrInvalidValue(u2, serving.UpdaterAnnotation).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier with spec changes",
		this: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
			},
			Spec: getServiceSpec("helloworld:bar"),
		},
		prev: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
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
