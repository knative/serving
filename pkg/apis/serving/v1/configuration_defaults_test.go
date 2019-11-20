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

	"github.com/google/go-cmp/cmp"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"

	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
)

func TestConfigurationDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Configuration
		want *Configuration
	}{{
		name: "empty",
		in:   &Configuration{},
		want: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
	}, {
		name: "run latest",
		in: &Configuration{
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
		want: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:           config.DefaultUserContainerName,
								Image:          "busybox",
								Resources:      defaultResources,
								ReadinessProbe: defaultProbe,
							}},
						},
						TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
	}, {
		name: "run latest with some default overrides",
		in: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						TimeoutSeconds:       ptr.Int64(60),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
		want: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:           config.DefaultUserContainerName,
								Image:          "busybox",
								Resources:      defaultResources,
								ReadinessProbe: defaultProbe,
							}},
						},
						TimeoutSeconds:       ptr.Int64(60),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			got.SetDefaults(context.Background())
			if !cmp.Equal(got, test.want, ignoreUnexportedResources) {
				t.Errorf("SetDefaults (-want, +got) = %v",
					cmp.Diff(test.want, got, ignoreUnexportedResources))
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						ContainerConcurrency: ptr.Int64(1),
					},
				},
			},
		},
		prev: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						ContainerConcurrency: ptr.Int64(2),
					},
				},
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
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						ContainerConcurrency: ptr.Int64(1),
					},
				},
			},
		}),
		prev: withUserAnns(u1, u2, &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						ContainerConcurrency: ptr.Int64(2),
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
