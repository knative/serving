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

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
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

func TestRouteUserInfo(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	withUserAnns := func(u1, u2 string, s *Route) *Route {
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
		this     *Route
		prev     *Route
		wantAnns map[string]string
	}{{
		name: "create-new",
		user: u1,
		this: &Route{},
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
		this:     &Route{},
		prev:     &Route{},
		wantAnns: map[string]string{},
	}, {
		name: "update-no-diff-new-object",
		user: u2,
		this: withUserAnns(u1, u1, &Route{}),
		prev: withUserAnns(u1, u1, &Route{}),
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u1,
		},
	}, {
		name: "update-diff-old-object",
		user: u2,
		this: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "new",
					},
				}},
			},
		},
		prev: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "old",
					},
				}},
			},
		},
		wantAnns: map[string]string{
			serving.UpdaterAnnotation: u2,
		},
	}, {
		name: "update-diff-new-object",
		user: u3,
		this: withUserAnns(u1, u2, &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "new",
					},
				}},
			},
		}),
		prev: withUserAnns(u1, u2, &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "old",
					},
				}},
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
