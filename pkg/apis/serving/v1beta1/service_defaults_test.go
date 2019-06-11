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

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/config"
	"github.com/knative/serving/pkg/apis/serving"
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
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:      config.DefaultUserContainerName,
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Percent:        100,
						LatestRevision: ptr.Bool(true),
					}},
				},
			},
		},
	}, {
		name: "run latest",
		in: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
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
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:      config.DefaultUserContainerName,
									Image:     "busybox",
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Percent:        100,
						LatestRevision: ptr.Bool(true),
					}},
				},
			},
		},
	}, {
		name: "run latest with some default overrides",
		in: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "busybox",
								}},
							},
							TimeoutSeconds: ptr.Int64(60),
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:      config.DefaultUserContainerName,
									Image:     "busybox",
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: ptr.Int64(60),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Percent:        100,
						LatestRevision: ptr.Bool(true),
					}},
				},
			},
		},
	}, {
		name: "byo traffic block",
		in: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
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
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Tag:          "current",
						RevisionName: "foo",
						Percent:      90,
					}, {
						Tag:          "candidate",
						RevisionName: "bar",
						Percent:      10,
					}, {
						Tag: "latest",
					}},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:      config.DefaultUserContainerName,
									Image:     "busybox",
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Tag:            "current",
						RevisionName:   "foo",
						Percent:        90,
						LatestRevision: ptr.Bool(false),
					}, {
						Tag:            "candidate",
						RevisionName:   "bar",
						Percent:        10,
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
					cmp.Diff(got, test.want, ignoreUnexportedResources))
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
			defer s.SetAnnotations(a)
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
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							ContainerConcurrency: 1,
						},
					},
				},
			},
		},
		prev: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							ContainerConcurrency: 2,
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
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							ContainerConcurrency: 1,
						},
					},
				},
			},
		}),
		prev: withUserAnns(u1, u2, &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							ContainerConcurrency: 2,
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
