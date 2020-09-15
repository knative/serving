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
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/equality"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving"
)

func (r *Route) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidateObjectMetadata(ctx, r.GetObjectMeta()).ViaField("metadata").
		Also(r.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec")).
		Also(validateAnnotations(r.GetAnnotations()))

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*Route)
		// Don't validate annotations(creator and lastModifier) when route owned by service
		// validate only when route created independently.
		if r.OwnerReferences == nil {
			errs = errs.Also(apis.ValidateCreatorAndModifier(original.Spec, r.Spec, original.GetAnnotations(),
				r.GetAnnotations(), serving.GroupName).ViaField("metadata.annotations"))
		}
	}
	return errs
}

func (rs *RouteSpec) Validate(ctx context.Context) *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &RouteSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}

	errs := apis.CheckDeprecated(ctx, rs)

	type diagnostic struct {
		index int
		field string
	}
	// Track the targets of named TrafficTarget entries (to detect duplicates).
	trafficMap := make(map[string]diagnostic)

	percentSum := int64(0)
	for i, tt := range rs.Traffic {
		// Delegate to the v1 validation.
		errs = errs.Also(tt.TrafficTarget.Validate(ctx).ViaFieldIndex("traffic", i))

		if tt.Percent != nil {
			percentSum += *tt.Percent
		}

		if tt.DeprecatedName != "" && tt.Tag != "" {
			errs = errs.Also(apis.ErrMultipleOneOf("name", "tag").
				ViaFieldIndex("traffic", i))
		} else if tt.DeprecatedName == "" && tt.Tag == "" {
			// No Name field, so skip the uniqueness check.
			continue
		}

		errs = errs.Also(apis.CheckDeprecated(ctx, tt).ViaFieldIndex("traffic", i))

		name := tt.DeprecatedName
		field := "name"
		if name == "" {
			name = tt.Tag
			field = "tag"
		}

		if d, ok := trafficMap[name]; !ok {
			// No entry exists, so add ours
			trafficMap[name] = diagnostic{i, field}
		} else {
			// We want only single definition of the route, even if it points
			// to the same config or revision.
			errs = errs.Also(&apis.FieldError{
				Message: fmt.Sprintf("Multiple definitions for %q", name),
				Paths: []string{
					fmt.Sprintf("traffic[%d].%s", d.index, d.field),
					fmt.Sprintf("traffic[%d].%s", i, field),
				},
			})
		}
	}

	if percentSum != 100 {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("Traffic targets sum to %d, want 100", percentSum),
			Paths:   []string{"traffic"},
		})
	}
	return errs
}

// TODO: this should be moved to knative/networking as part of
// https://github.com/knative/networking/issues/123
func validateAnnotations(annotations map[string]string) *apis.FieldError {
	disableAutoTLS := annotations[networking.DisableAutoTLSAnnotationKey]

	if disableAutoTLS != "" {
		if _, err := strconv.ParseBool(disableAutoTLS); err != nil {
			return apis.ErrInvalidValue(disableAutoTLS, networking.DisableAutoTLSAnnotationKey)
		}
	}

	return nil
}
