package v1alpha1

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

// Validate makes sure that DomainMapping is properly configured.
func (dm *DomainMapping) Validate(ctx context.Context) *apis.FieldError {
	return validateMetadata(ctx, dm.ObjectMeta).ViaField("metadata").
		Also(validateSpec(ctx, dm.Spec, dm.ObjectMeta).ViaField("spec"))
}

// validateSpec validates the Spec of a DomainMapping.
func validateSpec(ctx context.Context, spec DomainMappingSpec, md metav1.ObjectMeta) (errs *apis.FieldError) {
	return errs.Also(validateRef(ctx, spec.Ref, md).ViaField("ref"))
}

// validateRef validates the Ref field of a DomainMapping.
func validateRef(ctx context.Context, ref duckv1.KReference, md metav1.ObjectMeta) (errs *apis.FieldError) {
	if ref.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}

	if ref.Namespace != "" && ref.Namespace != md.Namespace {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("Ref namespace must be empty or equal to the domain mapping namespace %q", md.Namespace),
			Paths:   []string{"namespace"},
		})
	}

	return errs
}

// validateMetadata validates the metadata section of a DomainMapping.
func validateMetadata(ctx context.Context, md metav1.ObjectMeta) (errs *apis.FieldError) {
	if md.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}

	return errs
}
