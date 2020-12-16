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

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving"
)

// Validate makes sure that DomainMapping is properly configured.
func (dm *DomainMapping) Validate(ctx context.Context) *apis.FieldError {
	errs := dm.validateMetadata(ctx).ViaField("metadata")

	ctx = apis.WithinParent(ctx, dm.ObjectMeta)
	errs = errs.Also(dm.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))

	return errs
}

// validateMetadata validates the metadata section of a DomainMapping.
func (dm *DomainMapping) validateMetadata(ctx context.Context) (errs *apis.FieldError) {
	if dm.GenerateName != "" {
		errs = errs.Also(apis.ErrDisallowedFields("generateName"))
	}

	err := validation.IsFullyQualifiedDomainName(field.NewPath("name"), dm.Name)
	if err != nil {
		errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("invalid name %q: %s", dm.Name, err.ToAggregate()), "name"))
	}

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*DomainMapping)
		errs = errs.Also(
			apis.ValidateCreatorAndModifier(original.Spec, dm.Spec,
				original.GetAnnotations(), dm.GetAnnotations(), serving.GroupName).ViaField("annotations"),
		)
	}

	return errs
}

// Validate makes sure the DomainMappingSpec is properly configured.
func (spec *DomainMappingSpec) Validate(ctx context.Context) *apis.FieldError {
	return spec.Ref.Validate(ctx).ViaField("ref")
}
