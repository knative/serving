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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

// Validate makes sure that DomainMapping is properly configured.
func (dm *DomainMapping) Validate(_ context.Context) *apis.FieldError {
	return validateMetadata(&dm.ObjectMeta).ViaField("metadata").
		Also(dm.validateSpec().ViaField("spec"))
}

// validateSpec validates the Spec of a DomainMapping.
func (dm *DomainMapping) validateSpec() (errs *apis.FieldError) {
	return errs.Also(validateRef(&dm.Spec.Ref, dm.Namespace).ViaField("ref"))
}

// validateRef validates the Ref field of a DomainMapping.
func validateRef(ref *duckv1.KReference, ns string) (errs *apis.FieldError) {
	if ref.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}

	if ref.Namespace != "" && ref.Namespace != ns {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("Ref namespace must be empty or equal to the domain mapping namespace %q", ns),
			Paths:   []string{"namespace"},
		})
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
