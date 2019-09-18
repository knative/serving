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
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestServiceDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Service
		want *Service
	}{{
		name: "empty",
		in:   &Service{},
		want: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
							ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					}},
				},
			},
		},
	}, {
		name: "run latest",
		in: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "busybox",
								}},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
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
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					}},
				},
			},
		},
	}, {
		name: "run latest with some default overrides",
		in: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
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
		},
		want: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
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
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					}},
				},
			},
		},
	}, {
		name: "byo traffic block",
		in: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "busybox",
								}},
							},
						},
					},
				},
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						Tag:          "current",
						RevisionName: "foo",
						Percent:      ptr.Int64(90),
					}, {
						Tag:          "candidate",
						RevisionName: "bar",
						Percent:      ptr.Int64(10),
					}, {
						Tag: "latest",
					}},
				},
			},
		},
		want: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
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
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						Tag:            "current",
						RevisionName:   "foo",
						Percent:        ptr.Int64(90),
						LatestRevision: ptr.Bool(false),
					}, {
						Tag:            "candidate",
						RevisionName:   "bar",
						Percent:        ptr.Int64(10),
						LatestRevision: ptr.Bool(false),
					}, {
						Tag:            "latest",
						LatestRevision: ptr.Bool(true),
					}},
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

func TestAnnotateUserInfo(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)

	withUserAnns := func(u1, u2 string, s *Service) *Service {
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
		this     *Service
		prev     *Service
		wantAnns map[string]string
	}{{
		name: "create-new",
		user: u1,
		this: &Service{},
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
		this:     &Service{},
		prev:     &Service{},
		wantAnns: map[string]string{},
	}, {
		name: "update-no-diff-new-object",
		user: u2,
		this: withUserAnns(u1, u1, &Service{}),
		prev: withUserAnns(u1, u1, &Service{}),
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u1,
		},
	}, {
		name: "update-diff-old-object",
		user: u2,
		this: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							ContainerConcurrency: ptr.Int64(1),
						},
					},
				},
			},
		},
		prev: &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							ContainerConcurrency: ptr.Int64(2),
						},
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
		this: withUserAnns(u1, u2, &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							ContainerConcurrency: ptr.Int64(1),
						},
					},
				},
			},
		}),
		prev: withUserAnns(u1, u2, &Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							ContainerConcurrency: ptr.Int64(2),
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
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
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
