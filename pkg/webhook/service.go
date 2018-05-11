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
	"log"
	"reflect"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRollouts     = errors.New("The service must have exactly one of runLatest or pinned in spec field.")
	errMissingRevisionName = errors.New("The PinnedType must have revision specified.")
	errInvalidService      = errors.New("Failed to convert input into Route.")
)

func errServiceMissingField(fieldPath string) error {
	return fmt.Errorf("Service is missing %q", fieldPath)
}

func errServiceDisallowedFields(fieldPaths string) error {
	return fmt.Errorf("The service spec must not set the field(s): %s", fieldPaths)
}

// ValidateService is Service resource specific validation and mutation handler
func ValidateService(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	// We only care about the new one, old one gets flagged as an error in unmarshal.
	_, newService, err := unmarshalServiceCRDs(old, new, "ValidateService")
	if err != nil {
		return err
	}

	return validateSpec(newService)
}

func validateSpec(s *v1alpha1.Service) error {
	if s.Spec.RunLatest != nil && s.Spec.Pinned != nil ||
		s.Spec.RunLatest == nil && s.Spec.Pinned == nil {
		return errInvalidRollouts
	}
	if s.Spec.Pinned != nil {
		pinned := s.Spec.Pinned
		if len(pinned.RevisionName) == 0 {
			return errServiceMissingField("spec.pinned.revisionName")
		}
		if reflect.DeepEqual(pinned.Configuration, v1alpha1.ConfigurationSpec{}) {
			return errServiceMissingField("spec.pinned.configuration")
		}
		return validateConfigurationSpec(&pinned.Configuration)
	}
	runLatest := s.Spec.RunLatest
	if reflect.DeepEqual(runLatest.Configuration, v1alpha1.ConfigurationSpec{}) {
		return errServiceMissingField("spec.runLatest.configuration")
	}
	return validateConfigurationSpec(&runLatest.Configuration)
}

func unmarshalServiceCRDs(old GenericCRD, new GenericCRD, fnName string) (*v1alpha1.Service, *v1alpha1.Service, error) {
	var oldS *v1alpha1.Service
	if old != nil {
		var ok bool
		oldS, ok = old.(*v1alpha1.Service)
		if !ok {
			return nil, nil, fmt.Errorf("Failed to convert old into Service: %+v", old)
		}
	}
	log.Printf("%s: OLD Service is\n%+v", fnName, oldS)

	newS, ok := new.(*v1alpha1.Service)
	if !ok {
		return nil, nil, fmt.Errorf("Failed to convert new into Service: %+v", new)
	}
	log.Printf("%s: NEW Service is\n%+v", fnName, newS)

	return oldS, newS, nil
}
