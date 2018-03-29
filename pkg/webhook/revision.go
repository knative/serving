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
	"fmt"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/golang/glog"
	"github.com/mattbaird/jsonpatch"
)

// ValidateRevision is Revision resource specific validation and mutation handler
func ValidateRevision(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	_, _, err := unmarshal(old, new, "ValidateRevision")
	if err != nil {
		return err
	}

	return nil
}

func SetRevisionDefaults(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	_, newR, err := unmarshal(old, new, "SetRevisionDefaults")
	if err != nil {
		return err
	}

	if newR.Spec.ServingState == "" {
		*patches = append(*patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/spec/serving_state",
			Value:     v1alpha1.RevisionServingStateActive,
		})
	}

	return nil
}

func unmarshal(old GenericCRD, new GenericCRD, fnName string) (*v1alpha1.Revision, *v1alpha1.Revision, error) {
	var oldR *v1alpha1.Revision
	if old != nil {
		var ok bool
		oldR, ok = old.(*v1alpha1.Revision)
		if !ok {
			return nil, nil, fmt.Errorf("Failed to convert old into Revision: %+v", old)
		}
	}
	glog.Infof("%s: OLD Revision is\n%+v", fnName, oldR)

	newR, ok := new.(*v1alpha1.Revision)
	if !ok {
		return nil, nil, fmt.Errorf("Failed to convert new into Revision: %+v", new)
	}
	glog.Infof("%s: NEW Revision is\n%+v", fnName, newR)

	return oldR, newR, nil
}
