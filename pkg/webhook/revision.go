/*
Copyright 2017 Google Inc. All Rights Reserved.
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
	"fmt"
	"path"

	"github.com/knative/serving/pkg/apis/ela/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRevisionInput = errors.New("failed to convert input into Revision")
)

// ValidateRevision is Revision resource specific validation and mutation handler
func ValidateRevision(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		o, n, err := unmarshalRevisions(ctx, old, new, "ValidateRevision")
		if err != nil {
			return err
		}

		// When we have an "old" object, check for changes.
		if o != nil {
			// The autoscaler is allowed to change these fields, so clear them.
			o.Spec.ServingState = ""
			n.Spec.ServingState = ""

			if diff := cmp.Diff(o.Spec, n.Spec); diff != "" {
				return fmt.Errorf("Revision spec should not change (-old +new): %s", diff)
			}
		}

		return nil
	}
}

// SetRevisionDefaults set defaults on an revisions.
func SetRevisionDefaults(ctx context.Context) ResourceDefaulter {
	return func(patches *[]jsonpatch.JsonPatchOperation, crd GenericCRD) error {
		_, revision, err := unmarshalRevisions(ctx, nil, crd, "SetRevisionDefaults")
		if err != nil {
			return err
		}

		return setRevisionSpecDefaults(patches, "/spec", revision.Spec)
	}
}

func setRevisionSpecDefaults(patches *[]jsonpatch.JsonPatchOperation, patchBase string, spec v1alpha1.RevisionSpec) error {
	if spec.ServingState == "" {
		*patches = append(*patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      path.Join(patchBase, "servingState"),
			Value:     v1alpha1.RevisionServingStateActive,
		})
	}

	if spec.ConcurrencyModel == "" {
		*patches = append(*patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      path.Join(patchBase, "concurrencyModel"),
			Value:     v1alpha1.RevisionRequestConcurrencyModelMulti,
		})
	}

	return nil
}

func unmarshalRevisions(ctx context.Context, old GenericCRD, new GenericCRD, fnName string) (*v1alpha1.Revision, *v1alpha1.Revision, error) {
	logger := logging.FromContext(ctx)
	var oldRevision *v1alpha1.Revision
	if old != nil {
		var ok bool
		oldRevision, ok = old.(*v1alpha1.Revision)
		if !ok {
			return nil, nil, errInvalidRevisionInput
		}
	}
	logger.Infof("%s: OLD Revision is\n%+v", fnName, oldRevision)

	newRevision, ok := new.(*v1alpha1.Revision)
	if !ok {
		return nil, nil, errInvalidRevisionInput
	}
	logger.Infof("%s: NEW Revision is\n%+v", fnName, newRevision)

	return oldRevision, newRevision, nil
}
