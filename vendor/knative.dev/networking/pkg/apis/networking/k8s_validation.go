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

package networking

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"knative.dev/pkg/apis"
)

func ValidateNamespacedObjectReference(p *corev1.ObjectReference) *apis.FieldError {
	if p == nil {
		return nil
	}
	errs := apis.CheckDisallowedFields(*p, *NamespacedObjectReferenceMask(p))

	if p.APIVersion == "" {
		errs = errs.Also(apis.ErrMissingField("apiVersion"))
	} else if verrs := validation.IsQualifiedName(p.APIVersion); len(verrs) != 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(verrs, ", "), "apiVersion"))
	}
	if p.Kind == "" {
		errs = errs.Also(apis.ErrMissingField("kind"))
	} else if verrs := validation.IsCIdentifier(p.Kind); len(verrs) != 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(verrs, ", "), "kind"))
	}
	if p.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	} else if verrs := validation.IsDNS1123Label(p.Name); len(verrs) != 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(verrs, ", "), "name"))
	}
	return errs
}
