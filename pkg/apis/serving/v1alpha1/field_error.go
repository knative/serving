/*
Copyright 2017 The Knative Authors
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
	"fmt"
	"strings"
)

// currentField is a constant to supply as a fieldPath for when there is
// a problem with the current field itself.
const currentField = ""

// FieldError is used to propagate the context of errors pertaining to
// specific fields in a manner suitable for use in a recursive walk, so
// that errors contain the appropriate field context.
// +k8s:deepcopy-gen=false
type FieldError struct {
	Message string
	Paths   []string
	// Details contains an optional longer payload.
	Details string
}

// FieldError implements error
var _ error = (*FieldError)(nil)

// Validatable indicates that a particular type may have its fields validated.
type Validatable interface {
	// Validate checks the validity of this types fields.
	Validate() *FieldError
}

// HasImmutableFields indicates that a particular type has fields that should
// not change after creation.
type HasImmutableFields interface {
	// CheckImmutableFields checks that the current instance's immutable
	// fields haven't changed from the provided original.
	CheckImmutableFields(original HasImmutableFields) *FieldError
}

// ViaField is used to propagate a validation error along a field access.
// For example, if a type recursively validates its "spec" via:
//   if err := foo.Spec.Validate(); err != nil {
//     // Augment any field paths with the context that they were accessed
//     // via "spec".
//     return err.ViaField("spec")
//   }
func (fe *FieldError) ViaField(prefix ...string) *FieldError {
	if fe == nil {
		return nil
	}
	var newPaths []string
	for _, oldPath := range fe.Paths {
		if oldPath == currentField {
			newPaths = append(newPaths, strings.Join(prefix, "."))
		} else {
			newPaths = append(newPaths,
				strings.Join(append(prefix, oldPath), "."))
		}
	}
	fe.Paths = newPaths
	return fe
}

// Error implements error
func (fe *FieldError) Error() string {
	if fe.Details == "" {
		return fmt.Sprintf("%v: %v", fe.Message, strings.Join(fe.Paths, ", "))
	}
	return fmt.Sprintf("%v: %v\n%v", fe.Message, strings.Join(fe.Paths, ", "), fe.Details)
}

func errMissingField(fieldPaths ...string) *FieldError {
	return &FieldError{
		Message: "missing field(s)",
		Paths:   fieldPaths,
	}
}

func errDisallowedFields(fieldPaths ...string) *FieldError {
	return &FieldError{
		Message: "must not set the field(s)",
		Paths:   fieldPaths,
	}
}

func errInvalidValue(value string, fieldPath string) *FieldError {
	return &FieldError{
		Message: fmt.Sprintf("invalid value %q", value),
		Paths:   []string{fieldPath},
	}
}
