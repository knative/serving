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
	"github.com/knative/pkg/ptr"
	corev1 "k8s.io/api/core/v1"

	"github.com/knative/serving/pkg/apis/config"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
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
						TrafficTarget: v1beta1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        100,
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
								RevisionSpec: v1beta1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								DeprecatedContainer: &corev1.Container{
									Name:      config.DefaultUserContainerName,
									Resources: defaultResources,
								},
							},
						},
					},
				},
			},
		},
	}, {
		name: "run latest (lemonade)",
		wc:   v1beta1.WithUpgradeViaDefaulting,
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Image:     "busybox",
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        100,
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
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
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
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
									TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								DeprecatedContainer: &corev1.Container{
									Name:      config.DefaultUserContainerName,
									Resources: defaultResources,
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
								RevisionSpec: v1beta1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								DeprecatedContainer: &corev1.Container{
									Name:      config.DefaultUserContainerName,
									Resources: defaultResources,
								},
							},
						},
					},
				},
			},
		},
	}, {
		name: "pinned (lemonade)",
		wc:   v1beta1.WithUpgradeViaDefaulting,
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Image:     "busybox",
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							RevisionName:   "asdf",
							Percent:        100,
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
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
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
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
									TimeoutSeconds:       ptr.Int64(99),
								},
								DeprecatedContainer: &corev1.Container{
									Name:      config.DefaultUserContainerName,
									Resources: defaultResources,
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
								RevisionSpec: v1beta1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								DeprecatedContainer: &corev1.Container{
									Name:      config.DefaultUserContainerName,
									Resources: defaultResources,
								},
							},
						},
					},
				},
			},
		},
	}, {
		name: "release (double, lemonade)",
		wc:   v1beta1.WithUpgradeViaDefaulting,
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "current",
							Percent:        57,
							RevisionName:   "foo",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "candidate",
							Percent:        43,
							RevisionName:   "bar",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "latest",
							Percent:        0,
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
		},
	}, {
		name: "release (double, @latest, lemonade)",
		wc:   v1beta1.WithUpgradeViaDefaulting,
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "current",
							Percent:        57,
							RevisionName:   "foo",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "candidate",
							Percent:        43,
							LatestRevision: ptr.Bool(true),
						},
					}, {
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "latest",
							Percent:        0,
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
		},
	}, {
		name: "release (single, lemonade)",
		wc:   v1beta1.WithUpgradeViaDefaulting,
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "current",
							Percent:        100,
							RevisionName:   "foo",
							LatestRevision: ptr.Bool(false),
						},
					}, {
						TrafficTarget: v1beta1.TrafficTarget{
							Tag:            "latest",
							Percent:        0,
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
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
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
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
									TimeoutSeconds:       ptr.Int64(99),
								},
								DeprecatedContainer: &corev1.Container{
									Name:      config.DefaultUserContainerName,
									Resources: defaultResources,
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
							RevisionSpec: v1beta1.RevisionSpec{
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Image:     "blah",
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        100,
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
							},
							DeprecatedContainer: &corev1.Container{
								Name:      config.DefaultUserContainerName,
								Resources: defaultResources,
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        100,
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
							RevisionSpec: v1beta1.RevisionSpec{
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Image:     "blah",
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        100,
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
							RevisionSpec: v1beta1.RevisionSpec{
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
						TrafficTarget: v1beta1.TrafficTarget{
							Percent: 100,
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
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:      config.DefaultUserContainerName,
										Image:     "blah",
										Resources: defaultResources,
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        100,
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
