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
		name: "empty",
		in:   &Route{},
		want: &Route{},
	}, {
		name: "empty w/ default configuration",
		in:   &Route{},
		want: &Route{
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Percent:        ptr.Int64(100),
					LatestRevision: ptr.Bool(true),
				}},
			},
		},
		wc: v1.WithDefaultConfigurationName,
	}, {
		// Make sure it keeps a 'nil' as a 'nil' and not 'zero'
		name: "implied zero percent",
		in: &Route{
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Percent:        ptr.Int64(100),
					LatestRevision: ptr.Bool(true),
				}, {
					RevisionName: "bar",
				}},
			},
		},
		want: &Route{
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Percent:        ptr.Int64(100),
					LatestRevision: ptr.Bool(true),
				}, {
					RevisionName:   "bar",
					Percent:        nil,
					LatestRevision: ptr.Bool(false),
				}},
			},
		},
		wc: v1.WithDefaultConfigurationName,
	}, {
		// Just to make sure it doesn't convert a 'zero' into a 'nil'
		name: "explicit zero percent",
		in: &Route{
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Percent:        ptr.Int64(100),
					LatestRevision: ptr.Bool(true),
				}, {
					RevisionName: "bar",
					Percent:      ptr.Int64(0),
				}},
			},
		},
		want: &Route{
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Percent:        ptr.Int64(100),
					LatestRevision: ptr.Bool(true),
				}, {
					RevisionName:   "bar",
					Percent:        ptr.Int64(0),
					LatestRevision: ptr.Bool(false),
				}},
			},
		},
		wc: v1.WithDefaultConfigurationName,
	}, {
		name: "latest revision defaulting",
		in: &Route{
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					RevisionName: "foo",
					Percent:      ptr.Int64(12),
				}, {
					RevisionName: "bar",
					Percent:      ptr.Int64(34),
				}, {
					ConfigurationName: "baz",
					Percent:           ptr.Int64(54),
				}},
			},
		},
		want: &Route{
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					RevisionName:   "foo",
					Percent:        ptr.Int64(12),
					LatestRevision: ptr.Bool(false),
				}, {
					RevisionName:   "bar",
					Percent:        ptr.Int64(34),
					LatestRevision: ptr.Bool(false),
				}, {
					ConfigurationName: "baz",
					Percent:           ptr.Int64(54),
					LatestRevision:    ptr.Bool(true),
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
			if !cmp.Equal(test.want, got) {
				t.Errorf("SetDefaults (-want, +got) = %v",
					cmp.Diff(test.want, got))
			}
		})
	}
}
