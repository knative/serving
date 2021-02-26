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

package v1

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	network "knative.dev/networking/pkg"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
)

func TestTrafficTargetValidation(t *testing.T) {
	tests := []struct {
		name string
		tt   *TrafficTarget
		want *apis.FieldError
		wc   func(context.Context) context.Context
	}{{
		name: "valid with revisionName",
		tt: &TrafficTarget{
			RevisionName: "bar",
			Percent:      ptr.Int64(12),
		},
		wc: apis.WithinSpec,
	}, {
		name: "valid with revisionName and name (spec)",
		tt: &TrafficTarget{
			Tag:          "foo",
			RevisionName: "bar",
			Percent:      ptr.Int64(12),
		},
		wc: apis.WithinSpec,
	}, {
		name: "valid with revisionName and name (status)",
		tt: &TrafficTarget{
			Tag:          "foo",
			RevisionName: "bar",
			Percent:      ptr.Int64(12),
			URL: &apis.URL{
				Scheme: "http",
				Host:   "foo.bar.com",
			},
		},
		wc: apis.WithinStatus,
	}, {
		name: "invalid with revisionName and name (status)",
		tt: &TrafficTarget{
			Tag:          "foo",
			RevisionName: "bar",
			Percent:      ptr.Int64(12),
		},
		wc:   apis.WithinStatus,
		want: apis.ErrMissingField("url"),
	}, {
		name: "invalid with bad revisionName",
		tt: &TrafficTarget{
			RevisionName: "b ar",
			Percent:      ptr.Int64(12),
		},
		wc: apis.WithinSpec,
		want: apis.ErrInvalidKeyName(
			"b ar", "revisionName", "name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character (e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]')"),
	}, {
		name: "valid with revisionName and latestRevision",
		tt: &TrafficTarget{
			RevisionName:   "bar",
			LatestRevision: ptr.Bool(false),
			Percent:        ptr.Int64(12),
		},
		wc: apis.WithinSpec,
	}, {
		name: "invalid with revisionName and latestRevision (spec)",
		tt: &TrafficTarget{
			RevisionName:   "bar",
			LatestRevision: ptr.Bool(true),
			Percent:        ptr.Int64(12),
		},
		wc:   apis.WithinSpec,
		want: apis.ErrGeneric(`may not set revisionName "bar" when latestRevision is true`, "latestRevision"),
	}, {
		name: "valid with revisionName and latestRevision (status)",
		tt: &TrafficTarget{
			RevisionName:   "bar",
			LatestRevision: ptr.Bool(true),
			Percent:        ptr.Int64(12),
		},
		wc: apis.WithinStatus,
	}, {
		name: "valid with configurationName",
		tt: &TrafficTarget{
			ConfigurationName: "bar",
			Percent:           ptr.Int64(37),
		},
		wc: apis.WithinSpec,
	}, {
		name: "valid with configurationName and name (spec)",
		tt: &TrafficTarget{
			Tag:               "foo",
			ConfigurationName: "bar",
			Percent:           ptr.Int64(37),
		},
		wc: apis.WithinSpec,
	}, {
		name: "invalid with bad configurationName",
		tt: &TrafficTarget{
			ConfigurationName: "b ar",
			Percent:           ptr.Int64(37),
		},
		wc: apis.WithinSpec,
		want: apis.ErrInvalidKeyName(
			"b ar", "configurationName", "name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character (e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]')"),
	}, {
		name: "valid with configurationName and latestRevision",
		tt: &TrafficTarget{
			ConfigurationName: "blah",
			LatestRevision:    ptr.Bool(true),
			Percent:           ptr.Int64(37),
		},
		wc: apis.WithinSpec,
	}, {
		name: "invalid with configurationName and latestRevision",
		tt: &TrafficTarget{
			ConfigurationName: "blah",
			LatestRevision:    ptr.Bool(false),
			Percent:           ptr.Int64(37),
		},
		wc:   apis.WithinSpec,
		want: apis.ErrGeneric(`may not set revisionName "" when latestRevision is false`, "latestRevision"),
	}, {
		name: "invalid with configurationName and default configurationName",
		tt: &TrafficTarget{
			ConfigurationName: "blah",
			Percent:           ptr.Int64(37),
		},
		wc:   WithDefaultConfigurationName,
		want: apis.ErrDisallowedFields("configurationName"),
	}, {
		name: "valid with only default configurationName",
		tt: &TrafficTarget{
			Percent: ptr.Int64(37),
		},
		wc: func(ctx context.Context) context.Context {
			return WithDefaultConfigurationName(apis.WithinSpec(ctx))
		},
	}, {
		name: "valid with default configurationName and latestRevision",
		tt: &TrafficTarget{
			LatestRevision: ptr.Bool(true),
			Percent:        ptr.Int64(37),
		},
		wc: func(ctx context.Context) context.Context {
			return WithDefaultConfigurationName(apis.WithinSpec(ctx))
		},
	}, {
		name: "invalid with default configurationName and latestRevision",
		tt: &TrafficTarget{
			LatestRevision: ptr.Bool(false),
			Percent:        ptr.Int64(37),
		},
		wc: func(ctx context.Context) context.Context {
			return WithDefaultConfigurationName(apis.WithinSpec(ctx))
		},
		want: apis.ErrGeneric(`may not set revisionName "" when latestRevision is false`, "latestRevision"),
	}, {
		name: "valid with revisionName and default configurationName",
		tt: &TrafficTarget{
			RevisionName: "bar",
			Percent:      ptr.Int64(12),
		},
		wc: WithDefaultConfigurationName,
	}, {
		name: "valid with no percent",
		tt: &TrafficTarget{
			ConfigurationName: "booga",
		},
	}, {
		name: "valid with nil percent",
		tt: &TrafficTarget{
			ConfigurationName: "booga",
			Percent:           nil,
		},
	}, {
		name: "valid with zero percent",
		tt: &TrafficTarget{
			ConfigurationName: "booga",
			Percent:           ptr.Int64(0),
		},
	}, {
		name: "valid with no name",
		tt: &TrafficTarget{
			ConfigurationName: "booga",
			Percent:           ptr.Int64(100),
		},
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
			Percent: ptr.Int64(100),
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got neither",
			Paths:   []string{"revisionName", "configurationName"},
		},
	}, {
		name: "invalid percent too low",
		tt: &TrafficTarget{
			RevisionName: "foo",
			Percent:      ptr.Int64(-5),
		},
		want: apis.ErrOutOfBoundsValue("-5", "0", "100", "percent"),
	}, {
		name: "invalid percent too high",
		tt: &TrafficTarget{
			RevisionName: "foo",
			Percent:      ptr.Int64(101),
		},
		want: apis.ErrOutOfBoundsValue("101", "0", "100", "percent"),
	}, {
		name: "disallowed url set",
		tt: &TrafficTarget{
			ConfigurationName: "foo",
			Percent:           ptr.Int64(100),
			URL: &apis.URL{
				Host: "should.not.be.set",
			},
		},
		wc:   apis.WithinSpec,
		want: apis.ErrDisallowedFields("url"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got := test.tt.Validate(ctx)
			if !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}

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
					Tag:          "bar",
					RevisionName: "foo",
					Percent:      ptr.Int64(100),
				}},
			},
			Status: RouteStatus{
				RouteStatusFields: RouteStatusFields{
					Traffic: []TrafficTarget{{
						Tag:          "bar",
						RevisionName: "foo",
						Percent:      ptr.Int64(100),
						URL: &apis.URL{
							Scheme: "http",
							Host:   "bar.blah.com",
						},
					}},
				},
			},
		},
	}, {
		name: "valid split",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					Tag:          "prod",
					RevisionName: "foo",
					Percent:      ptr.Int64(90),
				}, {
					Tag:               "experiment",
					ConfigurationName: "bar",
					Percent:           ptr.Int64(10),
				}},
			},
		},
	}, {
		name: "valid split without tags",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					RevisionName: "foo",
					Percent:      ptr.Int64(90),
				}, {
					RevisionName: "bar",
					Percent:      ptr.Int64(10),
				}},
			},
		},
	}, {
		name: "invalid traffic entry (missing oneof)",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					Tag:     "foo",
					Percent: ptr.Int64(100),
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
		name: "invalid traffic entry (multiple names)",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					Tag:          "foo",
					RevisionName: "bar",
					Percent:      ptr.Int64(50),
				}, {
					Tag:          "foo",
					RevisionName: "bar",
					Percent:      ptr.Int64(50),
				}},
			},
		},
		want: &apis.FieldError{
			Message: `Multiple definitions for "foo"`,
			Paths: []string{
				"spec.traffic[0].tag",
				"spec.traffic[1].tag",
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
					Percent:      ptr.Int64(100),
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
					RevisionName: "foo",
					Percent:      ptr.Int64(90),
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
					RevisionName: "foo",
					Percent:      ptr.Int64(100),
				}},
			},
		},
		want: &apis.FieldError{
			Message: "not a DNS 1035 label: [must be no more than 63 characters]",
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "invalid tag name",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RouteSpec{
				Traffic: []TrafficTarget{{
					Tag:          "foo@",
					RevisionName: "bar",
					Percent:      ptr.Int64(100),
				}},
			},
		},
		want: &apis.FieldError{
			Message: "invalid value: not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"spec.traffic.tag[0]"},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}

func TestRouteLabelValidation(t *testing.T) {
	validRouteSpec := RouteSpec{
		Traffic: []TrafficTarget{{
			Tag:          "bar",
			RevisionName: "foo",
			Percent:      ptr.Int64(100),
		}},
	}
	tests := []struct {
		name string
		r    *Route
		want *apis.FieldError
	}{{
		name: "valid visibility name",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					network.VisibilityLabelKey: "cluster-local",
				},
			},
			Spec: validRouteSpec,
		},
	}, {
		name: "invalid visibility name",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					network.VisibilityLabelKey: "bad-value",
				},
			},
			Spec: validRouteSpec,
		},
		want: apis.ErrInvalidValue("bad-value", "metadata.labels."+network.VisibilityLabelKey),
	}, {
		name: "valid knative service name",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Service",
					Name:       "test-svc",
				}},
			},
			Spec: validRouteSpec,
		},
	}, {
		name: "invalid knative service name",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "absent-svc",
				},
			},
			Spec: validRouteSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/service"),
	}, {
		name: "Mismatch knative service label and owner ref",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "BrandNewService",
					Name:       "brand-new-svc",
				}},
			},
			Spec: validRouteSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/service"),
	}, {
		name: "invalid knative service name without correct owner ref",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Service",
					Name:       "absent-svc",
				}},
			},
			Spec: validRouteSpec,
		},
		want: apis.ErrMissingField("metadata.labels.serving.knative.dev/service"),
	}, {
		name: "invalid knative service name with multiple owner ref",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					serving.ServiceLabelKey: "test-svc",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "NewSerice",
					Name:       "test-new-svc",
				}, {
					APIVersion: "serving.knative.dev/v1alpha1",
					Kind:       "Service",
					Name:       "test-svc",
				}},
			},
			Spec: validRouteSpec,
		},
	}, {
		// We want to be able to introduce new labels with the serving prefix in the future
		// and not break downgrading.
		name: "allow unknown uses of knative.dev/serving prefix",
		r: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
				Labels: map[string]string{
					"serving.knative.dev/testlabel": "value",
				},
			},
			Spec: validRouteSpec,
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}

func getRouteSpec(confName string) RouteSpec {
	return RouteSpec{
		Traffic: []TrafficTarget{{
			LatestRevision:    ptr.Bool(true),
			Percent:           ptr.Int64(100),
			ConfigurationName: confName,
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
		name    string
		prev    *Route
		this    *Route
		wantErr *apis.FieldError
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
		wantErr: (&apis.FieldError{Message: "annotation value is immutable",
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
		wantErr: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier annotation without spec changes",
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
		wantErr: apis.ErrInvalidValue(u2, serving.UpdaterAnnotation).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier annotation with spec changes",
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
	}, {
		name: "rollout duration validation",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.RolloutDurationKey: "123s",
				},
			},
			Spec: getRouteSpec("new"),
		},
	}, {
		name: "rollout duration validation, fail",
		this: &Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.RolloutDurationKey: "three hours and seventeen seconds",
				},
			},
			Spec: getRouteSpec("new"),
		},
		wantErr: apis.ErrInvalidValue("three hours and seventeen seconds", serving.RolloutDurationKey).ViaField("metadata.annotations"),
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
					APIVersion: "v1",
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
					APIVersion: "v1",
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
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.prev != nil {
				ctx = apis.WithinUpdate(ctx, test.prev)
			}
			if diff := cmp.Diff(test.wantErr.Error(), test.this.Validate(ctx).Error()); diff != "" {
				t.Error("Validate (-want, +got) =", diff)
			}
		})
	}
}
