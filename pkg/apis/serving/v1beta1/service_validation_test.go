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

	"github.com/knative/pkg/apis"
)

func TestServiceValidation(t *testing.T) {
	boolTrue := true
	boolFalse := false

	goodConfigSpec := ConfigurationSpec{
		Template: RevisionTemplateSpec{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
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
			Spec: ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						LatestRevision: &boolTrue,
						Percent:        100,
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "valid release",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						Name:           "current",
						LatestRevision: &boolFalse,
						RevisionName:   "valid-00001",
						Percent:        98,
					}, {
						Name:           "candidate",
						LatestRevision: &boolFalse,
						RevisionName:   "valid-00002",
						Percent:        2,
					}, {
						Name:           "latest",
						LatestRevision: &boolTrue,
						Percent:        0,
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
			Spec: ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						ConfigurationName: "valid",
						LatestRevision:    &boolTrue,
						Percent:           100,
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
			Spec: ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						RevisionName:   "valid",
						LatestRevision: &boolTrue,
						Percent:        100,
					}},
				},
			},
		},
		want: apis.ErrInvalidValue(true, "spec.traffic[0].latestRevision"),
	}, {
		name: "invalid container concurrency",
		r: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: RevisionTemplateSpec{
						Spec: RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "busybox",
								}},
							},
							ContainerConcurrency: -10,
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						LatestRevision: &boolTrue,
						Percent:        100,
					}},
				},
			},
		},
		want: apis.ErrOutOfBoundsValue(
			-10, 0, RevisionContainerConcurrencyMax,
			"spec.template.spec.containerConcurrency"),
	}}

	// TODO(dangerd): PodSpec validation failures.
	// TODO(mattmoor): BYO revision name.

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
