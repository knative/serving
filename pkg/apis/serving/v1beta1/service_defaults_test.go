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

	"github.com/knative/serving/pkg/apis/config"
)

func TestServiceDefaulting(t *testing.T) {
	boolTrue := true
	boolFalse := false

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
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: intptr(config.DefaultRevisionTimeoutSeconds),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Percent:        100,
						LatestRevision: &boolTrue,
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
									Image:     "busybox",
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: intptr(config.DefaultRevisionTimeoutSeconds),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Percent:        100,
						LatestRevision: &boolTrue,
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
							TimeoutSeconds: intptr(60),
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
									Image:     "busybox",
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: intptr(60),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Percent:        100,
						LatestRevision: &boolTrue,
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
						Name:         "current",
						RevisionName: "foo",
						Percent:      90,
					}, {
						Name:         "candidate",
						RevisionName: "bar",
						Percent:      10,
					}, {
						Name: "latest",
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
									Image:     "busybox",
									Resources: defaultResources,
								}},
							},
							TimeoutSeconds: intptr(config.DefaultRevisionTimeoutSeconds),
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Name:           "current",
						RevisionName:   "foo",
						Percent:        90,
						LatestRevision: &boolFalse,
					}, {
						Name:           "candidate",
						RevisionName:   "bar",
						Percent:        10,
						LatestRevision: &boolFalse,
					}, {
						Name:           "latest",
						LatestRevision: &boolTrue,
					}},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			ctx := context.Background()
			got.SetDefaults(ctx)
			if diff := cmp.Diff(got, test.want, ignoreUnexportedResources); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}
}
