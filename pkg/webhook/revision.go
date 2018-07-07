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
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRevisionInput = errors.New("failed to convert input into Revision")

	// The autoscaler is allowed to change these fields, so clear them.
	ignoreServingState = cmpopts.IgnoreFields(v1alpha1.RevisionSpec{}, "ServingState")
)

// ValidateRevision is Revision resource specific validation and mutation handler
func ValidateRevision(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		o, err := unmarshalRevision(old)
		if err != nil {
			return err
		}
		if new == nil {
			return errInvalidRevisionInput
		}
		n, err := unmarshalRevision(new)
		if err != nil {
			return err
		}

		// When we have an "old" object, check for changes.
		if o != nil {
			if diff := cmp.Diff(o.Spec, n.Spec, ignoreServingState); diff != "" {
				return fmt.Errorf("Revision spec should not change (-old +new): %s", diff)
			}
		}

		// Can't just `return new.Validate()` because it doesn't properly nil-check.
		if err := new.Validate(); err != nil {
			return err
		}
		return nil
	}
}

// SetRevisionDefaults set defaults on an revisions.
func SetRevisionDefaults(ctx context.Context) ResourceDefaulter {
	return func(patches *[]jsonpatch.JsonPatchOperation, crd GenericCRD) error {
		rawOriginal, err := json.Marshal(crd)
		if err != nil {
			return err
		}
		crd.SetDefaults()

		// Marshal the before and after.
		rawRevision, err := json.Marshal(crd)
		if err != nil {
			return err
		}

		patch, err := jsonpatch.CreatePatch(rawOriginal, rawRevision)
		if err != nil {
			return err
		}
		*patches = append(*patches, patch...)
		return nil
	}
}

// TODO(mattmoor): Once we can put v1alpha1.Validatable and some Defaultable equivalent
// in GenericCRD we should be able to eliminate the need for this cast function.
func unmarshalRevision(crd GenericCRD) (rev *v1alpha1.Revision, err error) {
	if crd == nil {
		return
	}
	if asRev, ok := crd.(*v1alpha1.Revision); !ok {
		err = errInvalidRevisionInput
	} else {
		rev = asRev
	}
	return
}
