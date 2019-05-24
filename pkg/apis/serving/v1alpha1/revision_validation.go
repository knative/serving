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
	"strings"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/kmp"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (r *Revision) checkImmutableFields(ctx context.Context, original *Revision) *apis.FieldError {
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
	errs := serving.ValidateObjectMetadata(r.GetObjectMeta()).ViaField("metadata")
	errs = errs.Also(r.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
	if apis.IsInUpdate(ctx) {
		old := apis.GetBaseline(ctx).(*Revision)
		errs = errs.Also(r.checkImmutableFields(ctx, old))
	}
	return errs
}

// Validate ensures RevisionTemplateSpec is properly configured.
func (rt *RevisionTemplateSpec) Validate(ctx context.Context) *apis.FieldError {
	errs := rt.Spec.Validate(ctx).ViaField("spec")

	// If the DeprecatedRevisionTemplate has a name specified, then check that
	// it follows the requirements on the name.
	if rt.Name != "" {
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

		if !strings.HasPrefix(rt.Name, prefix) {
			errs = errs.Also(apis.ErrInvalidValue(
				fmt.Sprintf("%q must have prefix %q", rt.Name, prefix),
				"metadata.name"))
		}
	}

	// Validate the annotations of RevisionTemplate for sidecar resource request and limits
	errs = errs.Also(validateAnnotations(rt.Annotations))

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
			Paths:   []string{apis.CurrentField},
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
		volumes, err := serving.ValidateVolumes(rs.Volumes)
		if err != nil {
			errs = errs.Also(err.ViaField("volumes"))
		}
		errs = errs.Also(serving.ValidateContainer(
			*rs.DeprecatedContainer, volumes).ViaField("container"))
	default:
		errs = errs.Also(apis.ErrMissingOneOf("container", "containers"))
	}
	errs = errs.Also(serving.ValidateNamespacedObjectReference(rs.DeprecatedBuildRef).ViaField("buildRef"))

	if err := rs.DeprecatedConcurrencyModel.Validate(ctx).ViaField("concurrencyModel"); err != nil {
		errs = errs.Also(err)
	} else {
		errs = errs.Also(rs.ContainerConcurrency.Validate(ctx).ViaField("containerConcurrency"))
	}

	if rs.TimeoutSeconds != nil {
		errs = errs.Also(validateTimeoutSeconds(*rs.TimeoutSeconds))
	}
	return errs
}

func validateAnnotations(annotations map[string]string) *apis.FieldError {

	errs := validateResource(annotations, serving.QueueSideCarRequestCPUAnnotation, serving.QueueSideCarLimitCPUAnnotation)
	errs = errs.Also(validateResource(annotations, serving.QueueSideCarRequestMemoryAnnotation, serving.QueueSideCarLimitMemoryAnnotation))
	return errs
}

func validateResource(annotations map[string]string, requestAnnotationKey string, limitAnnotationKey string) *apis.FieldError {
	if len(annotations) == 0 {
		return nil
	}

	request, err := getQuantityFromAnnotations(annotations, requestAnnotationKey)
	if err != nil {
		return err
	}
	limit, err := getQuantityFromAnnotations(annotations, limitAnnotationKey)
	if err != nil {
		return err
	}

	if !limit.IsZero() && limit.Cmp(request) == -1 {
		return &apis.FieldError{
			Message: fmt.Sprintf("%s=%s is less than %s=%s", limitAnnotationKey, limit.String(), requestAnnotationKey, request.String()),
			Paths:   []string{limitAnnotationKey, requestAnnotationKey},
		}
	}

	return nil
}

func getQuantityFromAnnotations(m map[string]string, k string) (resource.Quantity, *apis.FieldError) {
	v, ok := m[k]
	if !ok {
		return resource.Quantity{}, nil
	}
	quantity, err := resource.ParseQuantity(v)
	if err != nil {
		return quantity, &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: %v", v, err),
			Paths:   []string{k},
		}
	}

	return quantity, nil
}

func validateTimeoutSeconds(timeoutSeconds int64) *apis.FieldError {
	if timeoutSeconds != 0 {
		if timeoutSeconds > int64(networking.DefaultTimeout.Seconds()) || timeoutSeconds < 0 {
			return apis.ErrOutOfBoundsValue(timeoutSeconds, 0,
				networking.DefaultTimeout.Seconds(),
				"timeoutSeconds")
		}
	}
	return nil
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

// Validate ensures RevisionRequestConcurrencyModelType is properly configured.
func (cm RevisionRequestConcurrencyModelType) Validate(ctx context.Context) *apis.FieldError {
	switch cm {
	case RevisionRequestConcurrencyModelType(""),
		RevisionRequestConcurrencyModelMulti,
		RevisionRequestConcurrencyModelSingle:
		return nil
	default:
		return apis.ErrInvalidValue(cm, apis.CurrentField)
	}
}
