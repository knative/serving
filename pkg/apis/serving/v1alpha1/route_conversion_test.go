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

package v1alpha1

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
)

func TestRouteConversionBadType(t *testing.T) {
	good, bad := &Route{}, &Service{}

	if err := good.ConvertUp(context.Background(), bad); err == nil {
		t.Errorf("ConvertUp() = %#v, wanted error", bad)
	}

	if err := good.ConvertDown(context.Background(), bad); err == nil {
		t.Errorf("ConvertDown() = %#v, wanted error", good)
	}
}

func TestRouteConversion(t *testing.T) {
	tests := []struct {
		name    string
		in      *Route
		wantErr bool
	}{{
		name: "config name",
		in: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1beta1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           100,
					},
				}},
			},
			Status: RouteStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 1,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				RouteStatusFields: RouteStatusFields{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							RevisionName: "foo-00001",
							Percent:      100,
						},
					}},
					// TODO(mattmoor): Addressable
					// TODO(mattmoor): Domain
					// TODO(mattmoor): DomainInternal
				},
			},
		},
	}, {
		name: "revision name",
		in: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 2,
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1beta1.TrafficTarget{
						RevisionName: "foo-00002",
						Percent:      100,
					},
				}},
			},
			Status: RouteStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 1,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				RouteStatusFields: RouteStatusFields{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							RevisionName: "foo-00001",
							Percent:      100,
						},
					}},
					// TODO(mattmoor): Addressable
					// TODO(mattmoor): Domain
					// TODO(mattmoor): DomainInternal
				},
			},
		},
	}, {
		name: "release",
		in: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 3,
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1beta1.TrafficTarget{
						RevisionName: "foo-00001",
						Percent:      90,
						Tag:          "current",
					},
				}, {
					TrafficTarget: v1beta1.TrafficTarget{
						RevisionName: "foo-00002",
						Percent:      10,
						Tag:          "candidate",
					},
				}, {
					TrafficTarget: v1beta1.TrafficTarget{
						ConfigurationName: "foo",
						Percent:           0,
						Tag:               "latest",
					},
				}},
			},
			Status: RouteStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 3,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				RouteStatusFields: RouteStatusFields{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1beta1.TrafficTarget{
							RevisionName: "foo-00001",
							Percent:      90,
							Tag:          "current",
							URL: &apis.URL{
								Scheme: "http",
								Host:   "current.foo.bar",
							},
						},
					}, {
						TrafficTarget: v1beta1.TrafficTarget{
							RevisionName: "foo-00002",
							Percent:      10,
							Tag:          "candidate",
							URL: &apis.URL{
								Scheme: "http",
								Host:   "candidate.foo.bar",
							},
						},
					}, {
						TrafficTarget: v1beta1.TrafficTarget{
							RevisionName: "foo-00003",
							Percent:      0,
							Tag:          "latest",
							URL: &apis.URL{
								Scheme: "http",
								Host:   "latest.foo.bar",
							},
						},
					}},
					// TODO(mattmoor): Addressable
					// TODO(mattmoor): Domain
					// TODO(mattmoor): DomainInternal
				},
			},
		},
	}, {
		name: "name and tag",
		in: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 3,
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "candidate",
					TrafficTarget: v1beta1.TrafficTarget{
						RevisionName: "foo-00001",
						Percent:      100,
						Tag:          "current",
					},
				}},
			},
			Status: RouteStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 3,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				RouteStatusFields: RouteStatusFields{
					Traffic: []TrafficTarget{},
				},
			},
		},
		wantErr: true,
	}}

	toDeprecated := func(in *Route) *Route {
		out := in.DeepCopy()
		for idx := range out.Spec.Traffic {
			out.Spec.Traffic[idx].DeprecatedName = out.Spec.Traffic[idx].Tag
			out.Spec.Traffic[idx].Tag = ""
		}
		for idx := range out.Status.Traffic {
			out.Status.Traffic[idx].DeprecatedName = out.Status.Traffic[idx].Tag
			out.Status.Traffic[idx].Tag = ""
		}
		return out
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beta := &v1beta1.Route{}
			if err := test.in.ConvertUp(context.Background(), beta); err != nil {
				if !test.wantErr {
					t.Errorf("ConvertUp() = %v", err)
				}
				return
			} else if test.wantErr {
				t.Errorf("ConvertUp() = %#v, wanted error", beta)
			}
			got := &Route{}
			if err := got.ConvertDown(context.Background(), beta); err != nil {
				t.Errorf("ConvertDown() = %v", err)
			}
			if diff := cmp.Diff(test.in, got); diff != "" {
				t.Errorf("roundtrip (-want, +got) = %v", diff)
			}
		})

		// A variant of the test that uses `name:`,
		// but end up with what we have above anyways.
		t.Run(test.name+" (deprecated)", func(t *testing.T) {
			if test.wantErr {
				t.Skip("skipping error rows")
			}
			start := toDeprecated(test.in)
			beta := &v1beta1.Route{}
			if err := start.ConvertUp(context.Background(), beta); err != nil {
				t.Errorf("ConvertUp() = %v", err)
			}
			got := &Route{}
			if err := got.ConvertDown(context.Background(), beta); err != nil {
				t.Errorf("ConvertDown() = %v", err)
			}
			if diff := cmp.Diff(test.in, got); diff != "" {
				t.Errorf("roundtrip (-want, +got) = %v", diff)
			}
		})
	}
}
