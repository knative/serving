/*
Copyright 2020 The Knative Authors

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

	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// Validate makes sure that DomainMapping is properly configured.
func (dm *DomainMapping) Validate(ctx context.Context) *apis.FieldError {
	errs := validateMetadata(&dm.ObjectMeta).ViaField("metadata")

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*DomainMapping)
		errs = errs.Also(
			apis.ValidateCreatorAndModifier(original.Spec, dm.Spec,
				original.GetAnnotations(), dm.GetAnnotations(), serving.GroupName).ViaField("metadata.annotations"),
		)
	}

	ctx = apis.WithinParent(ctx, dm.ObjectMeta)
	errs = errs.Also(dm.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
	return errs
}

// Validate makes sure the DomainMappingSpec is properly configured.
func (spec *DomainMappingSpec) Validate(ctx context.Context) *apis.FieldError {
	return spec.validateRef(ctx, spec.Ref).ViaField("ref")
}

// validateRef validates the Ref section of the DomainMappingSpec.
func (spec *DomainMappingSpec) validateRef(ctx context.Context, ref duckv1.KReference) *apis.FieldError {
	errs := ref.Validate(ctx)

	// For now, ref must be a serving.knative.dev/v1 Service.
	if ref.Kind != "Service" {
		errs = errs.Also(apis.ErrGeneric(`must be "Service"`, "kind"))
	}
	if ref.APIVersion != v1.SchemeGroupVersion.Identifier() {
		errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("must be %q", v1.SchemeGroupVersion.Identifier()), "apiVersion"))
	}

	// Since we currently construct the rewritten host from the name/namespace, make sure they're valid.
	ns := ref.Namespace
	if ns == "" {
		ns = apis.ParentMeta(ctx).Namespace
	}
	if msgs := validation.NameIsDNS1035Label(ref.Name, false); len(msgs) > 0 {
		errs = errs.Also(apis.ErrInvalidValue(fmt.Sprint("not a DNS 1035 label prefix: ", msgs), "name"))
	}
	if msgs := validation.ValidateNamespaceName(ns, false); len(msgs) > 0 {
		errs = errs.Also(apis.ErrInvalidValue(fmt.Sprint("not a valid namespace: ", msgs), "namespace"))
	}

	return errs
}

// validateMetadata validates the metadata section of a DomainMapping.
func validateMetadata(md *metav1.ObjectMeta) (errs *apis.FieldError) {
	if md.Name == "" {
		return errs.Also(apis.ErrMissingField("name"))
	}

	return nil
}
