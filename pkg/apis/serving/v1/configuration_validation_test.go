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
				t.Errorf("Validate (-want, +got) = %v", diff)
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
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
