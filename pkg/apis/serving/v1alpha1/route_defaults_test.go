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

	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestRouteDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Route
		want *Route
		wc   func(context.Context) context.Context
	}{{
		name: "simple",
		in: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "foo",
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
					},
				}, {
					TrafficTarget: v1.TrafficTarget{
						Tag:          "bar",
						RevisionName: "foo",
						Percent:      ptr.Int64(50),
					},
				}},
			},
		},
		want: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "foo",
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
						LatestRevision:    ptr.Bool(true),
					},
				}, {
					TrafficTarget: v1.TrafficTarget{
						Tag:            "bar",
						RevisionName:   "foo",
						Percent:        ptr.Int64(50),
						LatestRevision: ptr.Bool(false),
					},
				}},
			},
		},
	}, {
		name: "lemonade",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "foo",
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
					},
				}, {
					TrafficTarget: v1.TrafficTarget{
						Tag:          "bar",
						RevisionName: "foo",
						Percent:      ptr.Int64(50),
					},
				}},
			},
		},
		want: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						Tag:               "foo",
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
						LatestRevision:    ptr.Bool(true),
					},
				}, {
					TrafficTarget: v1.TrafficTarget{
						Tag:            "bar",
						RevisionName:   "foo",
						Percent:        ptr.Int64(50),
						LatestRevision: ptr.Bool(false),
					},
				}},
			},
		},
	}, {
		name: "lemonade (conflict)",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "foo",
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
					},
				}, {
					DeprecatedName: "baz",
					TrafficTarget: v1.TrafficTarget{
						Tag:          "bar",
						RevisionName: "foo",
						Percent:      ptr.Int64(50),
					},
				}},
			},
		},
		want: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "foo",
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
						LatestRevision:    ptr.Bool(true),
					},
				}, {
					DeprecatedName: "baz",
					TrafficTarget: v1.TrafficTarget{
						Tag:            "bar",
						RevisionName:   "foo",
						Percent:        ptr.Int64(50),
						LatestRevision: ptr.Bool(false),
					},
				}},
			},
		},
	}, {
		name: "lemonade (collision)",
		wc:   v1.WithUpgradeViaDefaulting,
		in: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "bar",
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
					},
				}, {
					TrafficTarget: v1.TrafficTarget{
						Tag:          "bar",
						RevisionName: "foo",
						Percent:      ptr.Int64(50),
					},
				}},
			},
		},
		want: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						Tag:               "bar",
						ConfigurationName: "foo",
						Percent:           ptr.Int64(50),
						LatestRevision:    ptr.Bool(true),
					},
				}, {
					TrafficTarget: v1.TrafficTarget{
						Tag:            "bar",
						RevisionName:   "foo",
						Percent:        ptr.Int64(50),
						LatestRevision: ptr.Bool(false),
					},
				}},
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
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}
}
