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
	"reflect"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/logging"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRollouts     = errors.New("the service must have exactly one of runLatest or pinned in spec field")
	errMissingRevisionName = errors.New("the PinnedType must have revision specified")
	errInvalidServiceInput = errors.New("failed to convert input into Service")
)

func errServiceMissingField(fieldPath string) error {
	return fmt.Errorf("Service is missing %q", fieldPath)
}

func errServiceDisallowedFields(fieldPaths string) error {
	return fmt.Errorf("The service spec must not set the field(s): %s", fieldPaths)
}

// ValidateService is Service resource specific validation and mutation handler
func ValidateService(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		// We only care about the new one, old one gets flagged as an error in unmarshal.
		_, newService, err := unmarshalServices(ctx, old, new, "ValidateService")
		if err != nil {
			return err
		}

		return validateSpec(newService)
	}
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

func unmarshalServices(
	ctx context.Context, old GenericCRD, new GenericCRD, fnName string) (*v1alpha1.Service, *v1alpha1.Service, error) {
	logger := logging.FromContext(ctx)
	var oldService *v1alpha1.Service
	if old != nil {
		var ok bool
		oldService, ok = old.(*v1alpha1.Service)
		if !ok {
			return nil, nil, errInvalidServiceInput
		}
	}
	logger.Infof("%s: OLD Service is\n%+v", fnName, oldService)

	newService, ok := new.(*v1alpha1.Service)
	if !ok {
		return nil, nil, errInvalidServiceInput
	}
	logger.Infof("%s: NEW Service is\n%+v", fnName, newService)

	return oldService, newService, nil
}

// SetServiceDefaults set defaults on an services.
// Service does not have any defaults, per-se, but because it holds a Configuration,
// we need to set the Configuration's defaults. SetServiceDefaults dispatches to
// SetConfigurationSpecDefaults to accomplish this.
func SetServiceDefaults(ctx context.Context) ResourceDefaulter {
	return func(patches *[]jsonpatch.JsonPatchOperation, crd GenericCRD) error {
		logger := logging.FromContext(ctx)
		_, service, err := unmarshalServices(ctx, nil, crd, "SetServiceDefaults")
		if err != nil {
			return err
		}

		var (
			configSpec v1alpha1.ConfigurationSpec
			patchBase  string
		)

		if service.Spec.RunLatest != nil {
			configSpec = service.Spec.RunLatest.Configuration
			patchBase = "/spec/runLatest/configuration"
		} else if service.Spec.Pinned != nil {
			configSpec = service.Spec.Pinned.Configuration
			patchBase = "/spec/pinned/configuration"
		} else {
			// We could error here, but validateSpec should catch this.
			logger.Info("could not find config in SetServiceDefaults")
			return nil
		}

		return setConfigurationSpecDefaults(patches, patchBase, configSpec)
	}
}
