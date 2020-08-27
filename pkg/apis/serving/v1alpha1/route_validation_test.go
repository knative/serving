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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"

	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestRouteValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *Route
		want *apis.FieldError
	}{{
		name: "valid",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						RevisionName: "foo",
						Percent:      ptr.Int64(100),
					},
				}},
			},
		},
		want: nil,
	}, {
		name: "valid split",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "prod",
					TrafficTarget: v1.TrafficTarget{
						RevisionName: "foo",
						Percent:      ptr.Int64(90),
					},
				}, {
					DeprecatedName: "experiment",
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "bar",
						Percent:           ptr.Int64(10),
					},
				}},
			},
		},
		want: nil,
	}, {
		name: "valid annotation",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					"networking.knative.dev/disableAutoTLS": "true",
				},
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						RevisionName: "foo",
						Percent:      ptr.Int64(100),
					},
				}},
			},
		},
		want: nil,
	}, {
		name: "invalid traffic entry",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					DeprecatedName: "foo",
					TrafficTarget: v1.TrafficTarget{
						Percent: ptr.Int64(100),
					},
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
					TrafficTarget: v1.TrafficTarget{
						RevisionName: "foo",
						Percent:      ptr.Int64(100),
					},
				}},
			},
		},
		want: &apis.FieldError{
			Message: "not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "invalid name - dots and spec percent is not 100",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "do.not.use.dots",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						RevisionName: "foo",
						Percent:      ptr.Int64(90),
					},
				}},
			},
		},
		want: (&apis.FieldError{
			Message: "not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"metadata.name"},
		}).Also(&apis.FieldError{
			Message: "Traffic targets sum to 90, want 100",
			Paths:   []string{"spec.traffic"},
		}),
	}, {
		name: "invalid name - too long",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("a", 64),
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						RevisionName: "foo",
						Percent:      ptr.Int64(100),
					},
				}},
			},
		},
		want: &apis.FieldError{
			Message: "not a DNS 1035 label: [must be no more than 63 characters]",
			Paths:   []string{"metadata.name"},
		}}, {
		name: "invalid annotation",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					"networking.knative.dev/disableAutoTLS": "foo",
				},
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					TrafficTarget: v1.TrafficTarget{
						RevisionName: "foo",
						Percent:      ptr.Int64(100),
					},
				}},
			},
		},
		want: &apis.FieldError{
			Message: "invalid value: foo",
			Paths:   []string{"networking.knative.dev/disableAutoTLS"},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.want.Error(), test.r.Validate(context.Background()).Error()); diff != "" {
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
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "foo",
					Percent:      ptr.Int64(100),
				},
			}},
		},
		want: nil,
	}, {
		name: "valid split",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				DeprecatedName: "prod",
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "foo",
					Percent:      ptr.Int64(90),
				},
			}, {
				DeprecatedName: "experiment",
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "bar",
					Percent:           ptr.Int64(10),
				},
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
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					Percent: ptr.Int64(100),
				},
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
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "b@r",
					Percent:      ptr.Int64(100),
				},
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
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "f**",
					Percent:           ptr.Int64(100),
				},
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
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "bar",
					Percent:      ptr.Int64(50),
				},
			}, {
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "baz",
					Percent:      ptr.Int64(50),
				},
			}},
		},
		want: multipleDefinitionError,
	}, {
		name: "collision (same revision)",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "bar",
					Percent:      ptr.Int64(50),
				},
			}, {
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "bar",
					Percent:      ptr.Int64(50),
				},
			}},
		},
		want: multipleDefinitionError,
	}, {
		name: "collision (same config)",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "bar",
					Percent:           ptr.Int64(50),
				},
			}, {
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "bar",
					Percent:           ptr.Int64(50),
				},
			}},
		},
		want: multipleDefinitionError,
	}, {
		name: "invalid total percentage",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "bar",
					Percent:      ptr.Int64(99),
				},
			}, {
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "baz",
					Percent:      ptr.Int64(99),
				},
			}},
		},
		want: &apis.FieldError{
			Message: "Traffic targets sum to 198, want 100",
			Paths:   []string{"traffic"},
		},
	}, {
		name: "multiple names",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					Tag:          "foo",
					RevisionName: "bar",
					Percent:      ptr.Int64(100),
				},
			}},
		},
		want: apis.ErrMultipleOneOf("traffic[0].name", "traffic[0].tag"),
	}, {
		name: "conflicting with different names",
		rs: &RouteSpec{
			Traffic: []TrafficTarget{{
				DeprecatedName: "foo",
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "bar",
					Percent:      ptr.Int64(50),
				},
			}, {
				TrafficTarget: v1.TrafficTarget{
					Tag:          "foo",
					RevisionName: "bar",
					Percent:      ptr.Int64(50),
				},
			}},
		},
		want: &apis.FieldError{
			Message: `Multiple definitions for "foo"`,
			Paths: []string{
				"traffic[0].name",
				"traffic[1].tag",
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.want.Error(), test.rs.Validate(context.Background()).Error()); diff != "" {
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
			DeprecatedName: "foo",
			TrafficTarget: v1.TrafficTarget{
				RevisionName: "bar",
				Percent:      ptr.Int64(12),
			},
		},
		want: nil,
	}, {
		name: "valid with name and configuration",
		tt: &TrafficTarget{
			DeprecatedName: "baz",
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "blah",
				Percent:           ptr.Int64(37),
			},
		},
		want: nil,
	}, {
		name: "valid with no percent",
		tt: &TrafficTarget{
			DeprecatedName: "ooga",
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "booga",
			},
		},
		want: nil,
	}, {
		name: "valid with no name",
		tt: &TrafficTarget{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "booga",
				Percent:           ptr.Int64(100),
			},
		},
		want: nil,
	}, {
		name: "invalid with both",
		tt: &TrafficTarget{
			TrafficTarget: v1.TrafficTarget{
				RevisionName:      "foo",
				ConfigurationName: "bar",
			},
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got both",
			Paths:   []string{"revisionName", "configurationName"},
		},
	}, {
		name: "invalid with neither",
		tt: &TrafficTarget{
			DeprecatedName: "foo",
			TrafficTarget: v1.TrafficTarget{
				Percent: ptr.Int64(100),
			},
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got neither",
			Paths:   []string{"revisionName", "configurationName"},
		},
	}, {
		name: "invalid percent too low",
		tt: &TrafficTarget{
			TrafficTarget: v1.TrafficTarget{
				RevisionName: "foo",
				Percent:      ptr.Int64(-5),
			},
		},
		want: apis.ErrOutOfBoundsValue(-5, 0, 100, "percent"),
	}, {
		name: "invalid percent too high",
		tt: &TrafficTarget{
			TrafficTarget: v1.TrafficTarget{
				RevisionName: "foo",
				Percent:      ptr.Int64(101),
			},
		},
		want: apis.ErrOutOfBoundsValue(101, 0, 100, "percent"),
	}, {
		name: "disallowed url set",
		tt: &TrafficTarget{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "foo",
				Percent:           ptr.Int64(100),
				URL: &apis.URL{
					Host: "should.not.be.set",
				},
			},
		},
		want: apis.ErrDisallowedFields("url"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.want.Error(), test.tt.Validate(context.Background()).Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func getRouteSpec(confName string) RouteSpec {
	return RouteSpec{
		Traffic: []TrafficTarget{{
			TrafficTarget: v1.TrafficTarget{
				LatestRevision:    ptr.Bool(true),
				Percent:           ptr.Int64(100),
				ConfigurationName: confName,
			},
		}},
	}
}

func TestRouteAnnotationUpdate(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	tests := []struct {
		name string
		prev *Route
		this *Route
		want *apis.FieldError
	}{{
		name: "update creator annotation",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("old"),
		},
		prev: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("old"),
		},
		want: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
	}, {
		name: "update creator annotation with spec changes",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("new"),
		},
		prev: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("old"),
		},
		want: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier without spec changes",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u2,
				},
			},
			Spec: getRouteSpec("old"),
		},
		prev: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("old"),
		},
		want: apis.ErrInvalidValue(u2, serving.UpdaterAnnotation).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier with spec changes",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
			},
			Spec: getRouteSpec("new"),
		},
		prev: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("old"),
		},
		want: nil,
	}, {
		name: "no validation for lastModifier annotation even after update without spec changes as route owned by service",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1alpha1",
					Kind:       serving.GroupName,
				}},
			},
			Spec: getRouteSpec("old"),
		},
		prev: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("old"),
		},
		want: nil,
	}, {
		name: "no validation for creator annotation even after update as route owned by service",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u3,
					serving.UpdaterAnnotation: u1,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1alpha1",
					Kind:       serving.GroupName,
				}},
			},
			Spec: getRouteSpec("old"),
		},
		prev: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getRouteSpec("old"),
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.prev)
			if diff := cmp.Diff(test.want.Error(), test.this.Validate(ctx).Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
