/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package accessor

import "strings"

// Error defines a type of error coming from Accessor.
type Error struct {
	err         error
	errorReason string
}

const (
	// NotOwnResource means the accessor does not own the resource.
	NotOwnResource string = "NotOwned"
)

// NewAccessorError creates a new accessor Error
func NewAccessorError(err error, reason string) Error {
	return Error{
		err:         err,
		errorReason: reason,
	}
}

func (a Error) Error() string {
	return strings.ToLower(string(a.errorReason)) + ": " + a.err.Error()
}

// IsNotOwned returns true if the error is caused by NotOwnResource.
func IsNotOwned(err error) bool {
	accessorError, ok := err.(Error)
	if !ok {
		return false
	}
	return accessorError.errorReason == NotOwnResource
}
