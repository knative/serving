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
				Manual: &ManualType{},
			},
		},
		// Manual does not take a configuration so do nothing
		want: &Service{
			Spec: ServiceSpec{
				Manual: &ManualType{},
			},
		},
	}, {
		name: "run latest",
		in: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1beta1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								Container: &corev1.Container{
									Resources: defaultResources,
								},
							},
						},
					},
				},
			},
		},
	}, {
		name: "run latest - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: &corev1.Container{},
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
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
									TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								Container: &corev1.Container{
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
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: &corev1.Container{},
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
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1beta1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								Container: &corev1.Container{
									Resources: defaultResources,
								},
							},
						},
					},
				},
			},
		},
	}, {
		name: "pinned - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: &corev1.Container{},
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
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
									TimeoutSeconds:       ptr.Int64(99),
								},
								Container: &corev1.Container{
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
				Release: &ReleaseType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: &corev1.Container{},
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				Release: &ReleaseType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1beta1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								},
								Container: &corev1.Container{
									Resources: defaultResources,
								},
							},
						},
					},
				},
			},
		},
	}, {
		name: "release - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				Release: &ReleaseType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								Container: &corev1.Container{},
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
				Release: &ReleaseType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1beta1.RevisionSpec{
									ContainerConcurrency: 1,
									TimeoutSeconds:       ptr.Int64(99),
								},
								Container: &corev1.Container{
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
					RevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1beta1.RevisionSpec{
								PodSpec: v1beta1.PodSpec{
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
					RevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: v1beta1.PodSpec{
									Containers: []corev1.Container{{
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
					RevisionTemplate: &RevisionTemplateSpec{},
				},
				// No RouteSpec should get defaulted to "run latest"
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					RevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
							},
							Container: &corev1.Container{
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
					RevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1beta1.RevisionSpec{
								PodSpec: v1beta1.PodSpec{
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
					RevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: v1beta1.PodSpec{
									Containers: []corev1.Container{{
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
					RevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1beta1.RevisionSpec{
								PodSpec: v1beta1.PodSpec{
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
					RevisionTemplate: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1beta1.RevisionSpec{
								TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
								PodSpec: v1beta1.PodSpec{
									Containers: []corev1.Container{{
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
			got.SetDefaults(context.Background())
			if diff := cmp.Diff(test.want, got, ignoreUnexportedResources); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}
}
