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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
)

func TestRouteValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *Route
		want *apis.FieldError
	}{{
		name: "valid",
		r: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					RevisionName: "foo",
					Percent:      100,
				}},
			},
		},
		want: nil,
	}, {
		name: "valid split",
		r: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					Name:         "prod",
					RevisionName: "foo",
					Percent:      90,
				}, {
					Name:              "experiment",
					ConfigurationName: "bar",
					Percent:           10,
				}},
			},
		},
		want: nil,
	}, {
		name: "invalid traffic entry",
		r: &Route{
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					Name:    "foo",
					Percent: 100,
				}},
			},
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got neither",
			Paths: []string{
				"spec.traffic[0].configurationName",
				"spec.traffic[0].revisionName",
			},
		},
	}, {
		name: "invalid name - dots",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "do.not.use.dots",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					RevisionName: "foo",
					Percent:      100,
				}},
			},
		},
		want: &apis.FieldError{Message: "Invalid resource name: special character . must not be present", Paths: []string{"metadata.name"}},
	}, {
		name: "invalid name - dots and spec percent is not 100",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "do.not.use.dots",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					RevisionName: "foo",
					Percent:      90,
				}},
			},
		},
		want: (&apis.FieldError{Message: "Invalid resource name: special character . must not be present", Paths: []string{"metadata.name"}}).
			Also(&apis.FieldError{Message: "Traffic targets sum to 90, want 100", Paths: []string{"spec.traffic"}}),
	}, {
		name: "invalid name - too long",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("a", 65),
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					RevisionName: "foo",
					Percent:      100,
				}},
			},
		},
		want: &apis.FieldError{Message: "Invalid resource name: length must be no more than 63 characters", Paths: []string{"metadata.name"}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRouteSpecValidation(t *testing.T) {
	multipleDefinitionError := &apis.FieldError{
		Message: `Multiple definitions for "foo"`,
		Paths:   []string{"traffic[0].name", "traffic[1].name"},
	}
	tests := []struct {
		name string
		rs   *RouteSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				RevisionName: "foo",
				Percent:      100,
			}},
		},
		want: nil,
	}, {
		name: "valid split",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				Name:         "prod",
				RevisionName: "foo",
				Percent:      90,
			}, {
				Name:              "experiment",
				ConfigurationName: "bar",
				Percent:           10,
			}},
		},
		want: nil,
	}, {
		name: "empty spec",
		rs:   &RouteSpec{},
		want: apis.ErrMissingField(apis.CurrentField),
	}, {
		name: "invalid traffic entry",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				Name:    "foo",
				Percent: 100,
			}},
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got neither",
			Paths: []string{
				"traffic[0].configurationName",
				"traffic[0].revisionName",
			},
		},
	}, {
		name: "invalid revision name",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				RevisionName: "b@r",
				Percent:      100,
			}},
		},
		want: &apis.FieldError{
			Message: `invalid key name "b@r"`,
			Paths:   []string{"traffic[0].revisionName"},
			Details: `name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character (e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]')`,
		},
	}, {
		name: "invalid revision name",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				ConfigurationName: "f**",
				Percent:           100,
			}},
		},
		want: &apis.FieldError{
			Message: `invalid key name "f**"`,
			Paths:   []string{"traffic[0].configurationName"},
			Details: `name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character (e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]')`,
		},
	}, {
		name: "invalid name conflict",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				Name:         "foo",
				RevisionName: "bar",
				Percent:      50,
			}, {
				Name:         "foo",
				RevisionName: "baz",
				Percent:      50,
			}},
		},
		want: multipleDefinitionError,
	}, {
		name: "collision (same revision)",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				Name:         "foo",
				RevisionName: "bar",
				Percent:      50,
			}, {
				Name:         "foo",
				RevisionName: "bar",
				Percent:      50,
			}},
		},
		want: multipleDefinitionError,
	}, {
		name: "collision (same config)",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				Name:              "foo",
				ConfigurationName: "bar",
				Percent:           50,
			}, {
				Name:              "foo",
				ConfigurationName: "bar",
				Percent:           50,
			}},
		},
		want: multipleDefinitionError,
	}, {
		name: "invalid total percentage",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				RevisionName: "bar",
				Percent:      99,
			}, {
				RevisionName: "baz",
				Percent:      99,
			}},
		},
		want: &apis.FieldError{
			Message: "Traffic targets sum to 198, want 100",
			Paths:   []string{"traffic"},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rs.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestTrafficTargetValidation(t *testing.T) {
	tests := []struct {
		name string
		tt   *TrafficTarget
		want *apis.FieldError
	}{{
		name: "valid with name and revision",
		tt: &TrafficTarget{
			Name:         "foo",
			RevisionName: "bar",
			Percent:      12,
		},
		want: nil,
	}, {
		name: "valid with name and configuration",
		tt: &TrafficTarget{
			Name:              "baz",
			ConfigurationName: "blah",
			Percent:           37,
		},
		want: nil,
	}, {
		name: "valid with no percent",
		tt: &TrafficTarget{
			Name:              "ooga",
			ConfigurationName: "booga",
		},
		want: nil,
	}, {
		name: "valid with no name",
		tt: &TrafficTarget{
			ConfigurationName: "booga",
			Percent:           100,
		},
		want: nil,
	}, {
		name: "invalid with both",
		tt: &TrafficTarget{
			RevisionName:      "foo",
			ConfigurationName: "bar",
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got both",
			Paths:   []string{"revisionName", "configurationName"},
		},
	}, {
		name: "invalid with neither",
		tt: &TrafficTarget{
			Name:    "foo",
			Percent: 100,
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got neither",
			Paths:   []string{"revisionName", "configurationName"},
		},
	}, {
		name: "invalid percent too low",
		tt: &TrafficTarget{
			RevisionName: "foo",
			Percent:      -5,
		},
		want: apis.ErrOutOfBoundsValue("-5", "0", "100", "percent"),
	}, {
		name: "invalid percent too high",
		tt: &TrafficTarget{
			RevisionName: "foo",
			Percent:      101,
		},
		want: apis.ErrOutOfBoundsValue("101", "0", "100", "percent"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.tt.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
