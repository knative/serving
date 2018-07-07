/*
Copyright 2018 The Knative Authors.
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

package webhook

import (
	"context"
	"errors"

	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRouteInput = errors.New("failed to convert input into Route")
)

// ValidateRoute is Route resource specific validation and mutation handler
func ValidateRoute(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		if new == nil {
			// TODO(mattmoor): This is a strange error to return here.  We can probably
			// just hoist the null handling into the caller?
			return errInvalidRouteInput
		}

		// Can't just `return newRoute.Validate()` because it doesn't properly nil-check.
		if err := new.Validate(); err != nil {
			return err
		}
		return nil
	}
}
