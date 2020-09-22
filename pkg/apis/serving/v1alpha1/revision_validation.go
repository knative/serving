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

	"k8s.io/apimachinery/pkg/api/equality"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmp"
	"knative.dev/serving/pkg/apis/autoscaling"
	apisconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
)

func (r *Revision) checkImmutableFields(original *Revision) *apis.FieldError {
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
	return nil
}

// Validate ensures Revision is properly configured.
func (r *Revision) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidateObjectMetadata(ctx, r.GetObjectMeta()).ViaField("metadata")
	if apis.IsInUpdate(ctx) {
		old := apis.GetBaseline(ctx).(*Revision)
		errs = errs.Also(r.checkImmutableFields(old))
	} else {
		errs = errs.Also(r.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
	}
	return errs
}

// Validate ensures RevisionTemplateSpec is properly configured.
func (rt *RevisionTemplateSpec) Validate(ctx context.Context) *apis.FieldError {
	allowZeroInitialScale := apisconfig.FromContextOrDefaults(ctx).Autoscaler.AllowZeroInitialScale
	errs := rt.Spec.Validate(ctx).ViaField("spec")
	errs = errs.Also(autoscaling.ValidateAnnotations(allowZeroInitialScale, rt.GetAnnotations()).ViaField("metadata.annotations"))
	// If the DeprecatedRevisionTemplate has a name specified, then check that
	// it follows the requirements on the name.
	errs = errs.Also(serving.ValidateRevisionName(ctx, rt.Name, rt.GenerateName))
	errs = errs.Also(serving.ValidateQueueSidecarAnnotation(rt.Annotations).ViaField("metadata.annotations"))
	return errs
}

// VerifyNameChange checks that if a user brought their own name previously that it
// changes at the appropriate times.
func (current *RevisionTemplateSpec) VerifyNameChange(ctx context.Context, og *RevisionTemplateSpec) *apis.FieldError {
	if current.Name == "" {
		// We only check that Name changes when the DeprecatedRevisionTemplate changes.
		return nil
	}
	if current.Name != og.Name {
		// The name changed, so we're good.
		return nil
	}

	if diff, err := kmp.ShortDiff(og, current); err != nil {
		return &apis.FieldError{
			Message: "Failed to diff DeprecatedRevisionTemplate",
			Paths:   []string{apis.CurrentField},
			Details: err.Error(),
		}
	} else if diff != "" {
		return &apis.FieldError{
			Message: "Saw the following changes without a name change (-old +new)",
			Paths:   []string{"metadata.name"},
			Details: diff,
		}
	}
	return nil
}

// Validate ensures RevisionSpec is properly configured.
func (rs *RevisionSpec) Validate(ctx context.Context) *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &RevisionSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}

	errs := apis.CheckDeprecated(ctx, rs)

	switch {
	case len(rs.PodSpec.Containers) > 0 && rs.DeprecatedContainer != nil:
		errs = errs.Also(apis.ErrMultipleOneOf("container", "containers"))
	case len(rs.PodSpec.Containers) > 0:
		errs = errs.Also(rs.RevisionSpec.Validate(ctx))
	case rs.DeprecatedContainer != nil:
		volumes, err := serving.ValidateVolumes(rs.Volumes, serving.AllMountedVolumes(append(rs.PodSpec.Containers, *rs.DeprecatedContainer)))
		if err != nil {
			errs = errs.Also(err.ViaField("volumes"))
		}
		errs = errs.Also(serving.ValidateContainer(ctx,
			*rs.DeprecatedContainer, volumes).ViaField("container"))
	default:
		errs = errs.Also(apis.ErrMissingOneOf("container", "containers"))
	}

	if rs.ContainerConcurrency != nil {
		errs = errs.Also(serving.ValidateContainerConcurrency(ctx, rs.ContainerConcurrency).ViaField("containerConcurrency"))
	}

	if rs.TimeoutSeconds != nil {
		errs = errs.Also(serving.ValidateTimeoutSeconds(ctx, *rs.TimeoutSeconds))
	}
	return errs
}

// Validate ensures DeprecatedRevisionServingStateType is properly configured.
func (ss DeprecatedRevisionServingStateType) Validate(ctx context.Context) *apis.FieldError {
	switch ss {
	case DeprecatedRevisionServingStateType(""),
		DeprecatedRevisionServingStateRetired,
		DeprecatedRevisionServingStateReserve,
		DeprecatedRevisionServingStateActive:
		return nil
	default:
		return apis.ErrInvalidValue(ss, apis.CurrentField)
	}
}
