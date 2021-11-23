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
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/validation"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmp"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
)

// Validate ensures Revision is properly configured.
func (r *Revision) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidateObjectMetadata(ctx, r.GetObjectMeta(), true).Also(
		r.ValidateLabels().ViaField("labels")).ViaField("metadata")
	errs = errs.Also(r.Status.Validate(apis.WithinStatus(ctx)).ViaField("status"))

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*Revision)
		if diff, err := kmp.ShortDiff(original.Spec, r.Spec); err != nil {
			return &apis.FieldError{
				Message: "Failed to diff Revision",
				Paths:   []string{"spec"},
				Details: err.Error(),
			}
		} else if diff != "" {
			return &apis.FieldError{
				Message: "Immutable fields changed (-old +new)",
				Paths:   []string{"spec"},
				Details: diff,
			}
		}
	} else {
		errs = errs.Also(r.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
	}

	return errs
}

// Validate implements apis.Validatable
func (rts *RevisionTemplateSpec) Validate(ctx context.Context) *apis.FieldError {
	errs := rts.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec")
	errs = errs.Also(autoscaling.ValidateAnnotations(ctx, config.FromContextOrDefaults(ctx).Autoscaler,
		rts.GetAnnotations()).ViaField("metadata.annotations"))

	// If the RevisionTemplateSpec has a name specified, then check that
	// it follows the requirements on the name.
	errs = errs.Also(validateRevisionName(ctx, rts.Name, rts.GenerateName))
	errs = errs.Also(validateQueueSidecarAnnotation(rts.Annotations).ViaField("metadata.annotations"))
	return errs
}

// VerifyNameChange checks that if a user brought their own name previously that it
// changes at the appropriate times.
func (rts *RevisionTemplateSpec) VerifyNameChange(_ context.Context, og *RevisionTemplateSpec) *apis.FieldError {
	if rts.Name == "" {
		// We only check that Name changes when the RevisionTemplate changes.
		return nil
	}
	if rts.Name != og.Name {
		// The name changed, so we're good.
		return nil
	}

	diff, err := kmp.ShortDiff(og, rts)
	if err != nil {
		return &apis.FieldError{
			Message: "Failed to diff RevisionTemplate",
			Paths:   []string{apis.CurrentField},
			Details: err.Error(),
		}
	}
	if diff != "" {
		return &apis.FieldError{
			Message: "Saw the following changes without a name change (-old +new)",
			Paths:   []string{"metadata.name"},
			Details: diff,
		}
	}
	return nil
}

// Validate implements apis.Validatable
func (rs *RevisionSpec) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidatePodSpec(ctx, rs.PodSpec)

	if rs.TimeoutSeconds != nil {
		errs = errs.Also(validateTimeoutSeconds(ctx, *rs.TimeoutSeconds))
	}

	if rs.ContainerConcurrency != nil {
		errs = errs.Also(serving.ValidateContainerConcurrency(ctx, rs.ContainerConcurrency).ViaField("containerConcurrency"))
	}

	return errs
}

// Validate implements apis.Validatable
func (rs *RevisionStatus) Validate(_ context.Context) *apis.FieldError {
	return nil
}

// ValidateLabels function validates service labels
func (r *Revision) ValidateLabels() (errs *apis.FieldError) {
	if val, ok := r.Labels[serving.ConfigurationLabelKey]; ok {
		errs = errs.Also(verifyLabelOwnerRef(val, serving.ConfigurationLabelKey, "Configuration", r.GetOwnerReferences()))
	}
	return errs
}

// validateRevisionName validates name and generateName for the revisionTemplate
func validateRevisionName(ctx context.Context, name, generateName string) *apis.FieldError {
	if generateName != "" {
		if msgs := validation.NameIsDNS1035Label(generateName, true); len(msgs) > 0 {
			return apis.ErrInvalidValue(
				fmt.Sprint("not a DNS 1035 label prefix: ", msgs),
				"metadata.generateName")
		}
	}
	if name != "" {
		if msgs := validation.NameIsDNS1035Label(name, false); len(msgs) > 0 {
			return apis.ErrInvalidValue(
				fmt.Sprint("not a DNS 1035 label: ", msgs),
				"metadata.name")
		}
		om := apis.ParentMeta(ctx)
		prefix := om.Name + "-"
		if om.Name != "" {
			// Even if there is GenerateName, allow the use
			// of Name post-creation.
		} else if om.GenerateName != "" {
			// We disallow bringing your own name when the parent
			// resource uses generateName (at creation).
			return apis.ErrDisallowedFields("metadata.name")
		}

		if !strings.HasPrefix(name, prefix) {
			return apis.ErrInvalidValue(
				fmt.Sprintf("%q must have prefix %q", name, prefix),
				"metadata.name")
		}
	}
	return nil
}

// validateTimeoutSeconds validates timeout by comparing MaxRevisionTimeoutSeconds
func validateTimeoutSeconds(ctx context.Context, timeoutSeconds int64) *apis.FieldError {
	if timeoutSeconds != 0 {
		cfg := config.FromContextOrDefaults(ctx)
		if timeoutSeconds > cfg.Defaults.MaxRevisionTimeoutSeconds || timeoutSeconds < 0 {
			return apis.ErrOutOfBoundsValue(timeoutSeconds, 0,
				cfg.Defaults.MaxRevisionTimeoutSeconds,
				"timeoutSeconds")
		}
	}
	return nil
}

// validateQueueSidecarAnnotation validates QueueSideCarResourcePercentageAnnotation
func validateQueueSidecarAnnotation(m map[string]string) *apis.FieldError {
	if len(m) == 0 {
		return nil
	}
	k, v, ok := serving.QueueSidecarResourcePercentageAnnotation.Get(m)
	if !ok {
		return nil
	}
	value, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return apis.ErrInvalidValue(v, apis.CurrentField).ViaKey(k)
	}
	if value < 0.1 || value > 100 {
		return apis.ErrOutOfBoundsValue(value, 0.1, 100.0, apis.CurrentField).ViaKey(k)
	}
	return nil
}
