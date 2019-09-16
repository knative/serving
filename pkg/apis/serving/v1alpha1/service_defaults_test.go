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
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/ptr"

	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestServiceDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Service
		want *Service
		wc   func(context.Context) context.Context
	}{{
		name: "empty",
		in:   &Service{},
		// When nothing is provided, we still get the "run latest" inline RouteSpec.
		want: &Service{
			Spec: ServiceSpec{
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
				},
			},
		},
	}, {
		name: "manual",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedManual: &ManualType{},
			},
		},
		// DeprecatedManual does not take a configuration so do nothing
		want: &Service{
			Spec: ServiceSpec{
				DeprecatedManual: &ManualType{},
			},
		},
	}, {
		name: "run latest",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
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
			},
		},
	}, {
		name: "run latest (lemonade)",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "busybox",
								},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
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
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
				},
			},
		},
	}, {
		name: "run latest - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
								RevisionSpec: v1.RevisionSpec{
									ContainerConcurrency: ptr.Int64(1),
									TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									ContainerConcurrency: ptr.Int64(1),
									TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
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
			},
		},
	}, {
		name: "pinned",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					Configuration: ConfigurationSpec{
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
			},
		},
	}, {
		name: "pinned (lemonade)",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "busybox",
								},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
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
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "asdf",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(false),
						},
					}},
				},
			},
		},
	}, {
		name: "pinned - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
								RevisionSpec: v1.RevisionSpec{
									ContainerConcurrency: ptr.Int64(1),
									TimeoutSeconds:       ptr.Int64(99),
								},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					Configuration: ConfigurationSpec{
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
			},
		},
	}, {
		name: "release",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"foo", "bar"},
					RolloutPercent: 43,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"foo", "bar"},
					RolloutPercent: 43,
					Configuration: ConfigurationSpec{
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
			},
		},
	}, {
		name: "release (double, lemonade)",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"foo", "bar"},
					RolloutPercent: 43,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
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
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:            "current",
							Percent:        ptr.Int64(57),
							RevisionName:   "foo",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "candidate",
							Percent:        ptr.Int64(43),
							RevisionName:   "bar",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							Percent:        nil,
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
		},
	}, {
		name: "release (double, @latest, lemonade)",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"foo", "@latest"},
					RolloutPercent: 43,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
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
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:            "current",
							Percent:        ptr.Int64(57),
							RevisionName:   "foo",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "candidate",
							Percent:        ptr.Int64(43),
							LatestRevision: ptr.Bool(true),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							Percent:        nil,
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
		},
	}, {
		name: "release (single, lemonade)",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{"foo"},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
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
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:            "current",
							Percent:        ptr.Int64(100),
							RevisionName:   "foo",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							Percent:        nil,
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
		},
	}, {
		name: "release - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{},
								RevisionSpec: v1.RevisionSpec{
									ContainerConcurrency: ptr.Int64(1),
									TimeoutSeconds:       ptr.Int64(99),
								},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Configuration: ConfigurationSpec{
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
			},
		},
	}, {
		name: "inline defaults to run latest",
		in: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					DeprecatedRevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "blah",
									}},
								},
							},
						},
					},
				},
				// No RouteSpec should get defaulted to "run latest"
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					DeprecatedRevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:           config.DefaultUserContainerName,
										Image:          "blah",
										Resources:      defaultResources,
										ReadinessProbe: defaultProbe,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
				},
			},
		},
	}, {
		name: "inline with empty RevisionSpec",
		in: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					DeprecatedRevisionTemplate: &RevisionTemplateSpec{},
				},
				// No RouteSpec should get defaulted to "run latest"
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
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
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
				},
			},
		},
	}, {
		name: "inline defaults to run latest (non-nil traffic)",
		in: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					DeprecatedRevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "blah",
									}},
								},
							},
						},
					},
				},
				// No RouteSpec should get defaulted to "run latest"
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					DeprecatedRevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:           config.DefaultUserContainerName,
										Image:          "blah",
										Resources:      defaultResources,
										ReadinessProbe: defaultProbe,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
				},
			},
		},
	}, {
		name: "inline with just percent",
		in: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					DeprecatedRevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "blah",
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Percent: ptr.Int64(100),
						},
					}},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					DeprecatedRevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:           config.DefaultUserContainerName,
										Image:          "blah",
										Resources:      defaultResources,
										ReadinessProbe: defaultProbe,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
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
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{},
			},
		},
		prev: &Service{
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{},
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
				DeprecatedRelease: &ReleaseType{},
			},
		}),
		prev: withUserAnns(u1, u2, &Service{
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{},
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
