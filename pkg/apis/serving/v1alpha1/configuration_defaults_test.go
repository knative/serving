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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"

	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

var (
	defaultResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	ignoreUnexportedResources = cmpopts.IgnoreUnexported(resource.Quantity{})
)

func TestConfigurationDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Configuration
		want *Configuration
		wc   func(context.Context) context.Context
	}{{
		name: "empty",
		in:   &Configuration{},
		want: &Configuration{},
	}, {
		name: "shell",
		in: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{},
					},
				},
			},
		},
		want: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
							ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
						},
						DeprecatedContainer: &corev1.Container{
							Name:           config.DefaultUserContainerName,
							Resources:      defaultResources,
							ReadinessProbe: defaultProbe,
						},
					},
				},
			},
		},
	}, {
		name: "lemonade",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
		want: &Configuration{
			Spec: ConfigurationSpec{
				Template: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
							ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:           config.DefaultUserContainerName,
									Image:          "busybox",
									Resources:      defaultResources,
									ReadinessProbe: defaultProbe,
								}},
							},
						},
					},
				},
			},
		},
	}, {
		name: "shell podspec",
		in: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{}},
							},
						},
					},
				},
			},
		},
		want: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
							ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:           config.DefaultUserContainerName,
									Resources:      defaultResources,
									ReadinessProbe: defaultProbe,
								}},
							},
						},
					},
				},
			},
		},
	}, {
		name: "no overwrite values",
		in: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							ContainerConcurrency: ptr.Int64(1),
							TimeoutSeconds:       ptr.Int64(99),
						},
						DeprecatedContainer: &corev1.Container{
							Resources:      defaultResources,
							ReadinessProbe: defaultProbe,
						},
					},
				},
			},
		},
		want: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							ContainerConcurrency: ptr.Int64(1),
							TimeoutSeconds:       ptr.Int64(99),
						},
						DeprecatedContainer: &corev1.Container{
							Name:           config.DefaultUserContainerName,
							Resources:      defaultResources,
							ReadinessProbe: defaultProbe,
						},
					},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			ctx := context.Background()
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got.SetDefaults(ctx)
			if diff := cmp.Diff(test.want, got, ignoreUnexportedResources); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}
}

func TestConfigurationUserInfo(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	withUserAnns := func(u1, u2 string, s *Configuration) *Configuration {
		a := s.GetAnnotations()
		if a == nil {
			a = map[string]string{}
			s.SetAnnotations(a)
		}
		a[serving.CreatorAnnotation] = u1
		a[serving.UpdaterAnnotation] = u2
		return s
	}
	tests := []struct {
		name     string
		user     string
		this     *Configuration
		prev     *Configuration
		wantAnns map[string]string
	}{{
		name: "create-new",
		user: u1,
		this: &Configuration{},
		prev: nil,
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u1,
		},
	}, {
		// Old objects don't have the annotation, and unless there's a change in
		// data they won't get it.
		name:     "update-no-diff-old-object",
		user:     u1,
		this:     &Configuration{},
		prev:     &Configuration{},
		wantAnns: map[string]string{},
	}, {
		name: "update-no-diff-new-object",
		user: u2,
		this: withUserAnns(u1, u1, &Configuration{}),
		prev: withUserAnns(u1, u1, &Configuration{}),
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u1,
		},
	}, {
		name: "update-diff-old-object",
		user: u2,
		this: &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{},
			},
		},
		prev: &Configuration{
			Spec: ConfigurationSpec{
				Template: &RevisionTemplateSpec{},
			},
		},
		wantAnns: map[string]string{
			serving.UpdaterAnnotation: u2,
		},
	}, {
		name: "update-diff-new-object",
		user: u3,
		this: withUserAnns(u1, u2, &Configuration{
			Spec: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "busybox",
								}},
							},
						},
					},
				},
			},
		}),
		prev: withUserAnns(u1, u2, &Configuration{
			Spec: ConfigurationSpec{
				Template: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						RevisionSpec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "helloworld",
								}},
							},
						},
					},
				},
			},
		}),
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u3,
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := apis.WithUserInfo(context.Background(), &authv1.UserInfo{
				Username: test.user,
			})
			if test.prev != nil {
				ctx = apis.WithinUpdate(ctx, test.prev)
				test.prev.SetDefaults(ctx)
			}
			test.this.SetDefaults(ctx)
			if got, want := test.this.GetAnnotations(), test.wantAnns; !cmp.Equal(got, want) {
				t.Errorf("Annotations = %v, want: %v, diff (-got, +want): %s", got, want, cmp.Diff(got, want))
			}
		})
	}
}
