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
	"reflect"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/serving"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/knative/serving/pkg/apis/serving/v1beta1"
)

// Validate validates the fields belonging to Service
func (s *Service) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidateObjectMetadata(s.GetObjectMeta()).ViaField("metadata")
	ctx = apis.WithinParent(ctx, s.ObjectMeta)
	errs = errs.Also(s.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*Service)

		field, currentConfig := s.Spec.getConfigurationSpec()
		_, originalConfig := original.Spec.getConfigurationSpec()

		if currentConfig != nil && originalConfig != nil {
			err := currentConfig.GetTemplate().VerifyNameChange(ctx,
				originalConfig.GetTemplate())
			errs = errs.Also(err.ViaField(
				// TODO(mattmoor): revisionTemplate -> field
				"spec", field, "configuration", "revisionTemplate"))
		}
	}

	return errs
}

func (ss *ServiceSpec) getConfigurationSpec() (string, *ConfigurationSpec) {
	switch {
	case ss.RunLatest != nil:
		return "runLatest", &ss.RunLatest.Configuration
	case ss.Release != nil:
		return "release", &ss.Release.Configuration
	case ss.Manual != nil:
		return "", nil
	case ss.DeprecatedPinned != nil:
		return "pinned", &ss.DeprecatedPinned.Configuration
	default:
		return "", nil
	}
}

// CheckDeprecated checks whether the provided named deprecated fields
// are set in a context where deprecation is disallowed.
func CheckDeprecated(ctx context.Context, fields map[string]interface{}) *apis.FieldError {
	if apis.IsDeprecatedAllowed(ctx) {
		return nil
	}
	var errs *apis.FieldError
	for name, field := range fields {
		// From: https://stackoverflow.com/questions/13901819/quick-way-to-detect-empty-values-via-reflection-in-go
		if !reflect.DeepEqual(field, reflect.Zero(reflect.TypeOf(field)).Interface()) {
			errs = errs.Also(apis.ErrDisallowedFields(name))
		}
	}
	return errs
}

// Validate validates the fields belonging to ServiceSpec recursively
func (ss *ServiceSpec) Validate(ctx context.Context) *apis.FieldError {
	// We would do this semantic DeepEqual, but the spec is comprised
	// entirely of a oneof, the validation for which produces a clearer
	// error message.
	// if equality.Semantic.DeepEqual(ss, &ServiceSpec{}) {
	// 	return apis.ErrMissingField(currentField)
	// }

	errs := CheckDeprecated(ctx, map[string]interface{}{
		"generation": ss.DeprecatedGeneration,
		"pinned":     ss.DeprecatedPinned,
		// TODO(mattmoor): "runLatest": ss.RunLatest,
		// TODO(mattmoor): "release": ss.Release,
		// TODO(mattmoor): "manual": ss.Manual,
	})

	set := []string{}

	if ss.RunLatest != nil {
		set = append(set, "runLatest")
		errs = errs.Also(ss.RunLatest.Validate(ctx).ViaField("runLatest"))
	}
	if ss.Release != nil {
		set = append(set, "release")
		errs = errs.Also(ss.Release.Validate(ctx).ViaField("release"))
	}
	if ss.Manual != nil {
		set = append(set, "manual")
		errs = errs.Also(ss.Manual.Validate(ctx).ViaField("manual"))
	}
	if ss.DeprecatedPinned != nil {
		set = append(set, "pinned")
		errs = errs.Also(ss.DeprecatedPinned.Validate(ctx).ViaField("pinned"))
	}

	// Before checking ConfigurationSpec, check RouteSpec.
	if len(set) > 0 && len(ss.RouteSpec.Traffic) > 0 {
		errs = errs.Also(apis.ErrMultipleOneOf(
			append([]string{"traffic"}, set...)...))
	}

	if !equality.Semantic.DeepEqual(ss.ConfigurationSpec, ConfigurationSpec{}) {
		set = append(set, "template")

		// Disallow the use of deprecated fields within our inlined
		// Configuration and Route specs.
		ctx = apis.DisallowDeprecated(ctx)

		errs = errs.Also(ss.ConfigurationSpec.Validate(ctx))
		errs = errs.Also(ss.RouteSpec.Validate(
			// Within the context of Service, the RouteSpec has a default
			// configurationName.
			v1beta1.WithDefaultConfigurationName(ctx)))
	}

	if len(set) > 1 {
		errs = errs.Also(apis.ErrMultipleOneOf(set...))
	} else if len(set) == 0 {
		errs = errs.Also(apis.ErrMissingOneOf("runLatest", "release", "manual",
			"pinned", "template"))
	}
	return errs
}

// Validate validates the fields belonging to PinnedType
func (pt *PinnedType) Validate(ctx context.Context) *apis.FieldError {
	var errs *apis.FieldError
	if pt.RevisionName == "" {
		errs = apis.ErrMissingField("revisionName")
	}
	return errs.Also(pt.Configuration.Validate(ctx).ViaField("configuration"))
}

// Validate validates the fields belonging to RunLatestType
func (rlt *RunLatestType) Validate(ctx context.Context) *apis.FieldError {
	return rlt.Configuration.Validate(ctx).ViaField("configuration")
}

// Validate validates the fields belonging to ManualType
func (m *ManualType) Validate(ctx context.Context) *apis.FieldError {
	return nil
}

// Validate validates the fields belonging to ReleaseType
func (rt *ReleaseType) Validate(ctx context.Context) *apis.FieldError {
	var errs *apis.FieldError

	numRevisions := len(rt.Revisions)
	if numRevisions == 0 {
		errs = errs.Also(apis.ErrMissingField("revisions"))
	}
	if numRevisions > 2 {
		errs = errs.Also(apis.ErrOutOfBoundsValue(numRevisions, 1, 2, "revisions"))
	}
	for i, r := range rt.Revisions {
		// Skip over the last revision special keyword.
		if r == ReleaseLatestRevisionKeyword {
			continue
		}
		if msgs := validation.IsDNS1035Label(r); len(msgs) > 0 {
			errs = errs.Also(apis.ErrInvalidArrayValue(
				fmt.Sprintf("not a DNS 1035 label: %v", msgs),
				"revisions", i))
		}
	}

	if numRevisions < 2 && rt.RolloutPercent != 0 {
		errs = errs.Also(apis.ErrInvalidValue(rt.RolloutPercent, "rolloutPercent"))
	}

	if rt.RolloutPercent < 0 || rt.RolloutPercent > 99 {
		errs = errs.Also(apis.ErrOutOfBoundsValue(
			rt.RolloutPercent, 0, 99,
			"rolloutPercent"))
	}

	return errs.Also(rt.Configuration.Validate(ctx).ViaField("configuration"))
}
