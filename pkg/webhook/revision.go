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
	"errors"
	"fmt"
	"path"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/golang/glog"
	"github.com/google/go-cmp/cmp"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRevisionInput = errors.New("Failed to convert input into Revision.")
)

// ValidateRevision is Revision resource specific validation and mutation handler
func ValidateRevision(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	o, n, err := unmarshalRevisions(old, new, "ValidateRevision")
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

func SetRevisionDefaults(patches *[]jsonpatch.JsonPatchOperation, crd GenericCRD) error {
	_, revision, err := unmarshalRevisions(nil, crd, "SetRevisionDefaults")
	if err != nil {
		return err
	}

	return SetRevisionSpecDefaults(patches, "/spec", revision.Spec)
}

func SetRevisionSpecDefaults(patches *[]jsonpatch.JsonPatchOperation, patchBase string, spec v1alpha1.RevisionSpec) error {
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

func unmarshalRevisions(old GenericCRD, new GenericCRD, fnName string) (*v1alpha1.Revision, *v1alpha1.Revision, error) {
	var oldRevision *v1alpha1.Revision
	if old != nil {
		var ok bool
		oldRevision, ok = old.(*v1alpha1.Revision)
		if !ok {
			return nil, nil, errInvalidRevisionInput
		}
	}
	glog.Infof("%s: OLD Revision is\n%+v", fnName, oldRevision)

	newRevision, ok := new.(*v1alpha1.Revision)
	if !ok {
		return nil, nil, errInvalidRevisionInput
	}
	glog.Infof("%s: NEW Revision is\n%+v", fnName, newRevision)

	return oldRevision, newRevision, nil
}
