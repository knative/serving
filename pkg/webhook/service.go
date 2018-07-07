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

package webhook

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidServiceInput = errors.New("failed to convert input into Service")
)

// ValidateService is Service resource specific validation and mutation handler
func ValidateService(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		if new == nil {
			// TODO(mattmoor): This is a strange error to return here.  We can probably
			// just hoist the null handling into the caller?
			return errInvalidServiceInput
		}

		// Can't just `return new.Validate()` because it doesn't properly nil-check.
		if err := new.Validate(); err != nil {
			return err
		}
		return nil
	}
}

// SetServiceDefaults set defaults on an services.
// Service does not have any defaults, per-se, but because it holds a Configuration,
// we need to set the Configuration's defaults. SetServiceDefaults dispatches to
// SetConfigurationSpecDefaults to accomplish this.
func SetServiceDefaults(ctx context.Context) ResourceDefaulter {
	return func(patches *[]jsonpatch.JsonPatchOperation, crd GenericCRD) error {
		rawOriginal, err := json.Marshal(crd)
		if err != nil {
			return err
		}
		crd.SetDefaults()

		// Marshal the before and after.
		rawService, err := json.Marshal(crd)
		if err != nil {
			return err
		}

		patch, err := jsonpatch.CreatePatch(rawOriginal, rawService)
		if err != nil {
			return err
		}
		*patches = append(*patches, patch...)
		return nil
	}
}
