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
	"reflect"

	"github.com/golang/glog"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/mattbaird/jsonpatch"
)

const (
	errEmptySpecInConfigurationMessage      = "The revision template must have revision template spec"
	errEmptyTemplateInSpecMessage           = "The revision template spec must have revision template"
	errNonEmptyStatusInConfigurationMessage = "The revision template cannot have status when it is created"
)

// ValidateConfiguration is Configuration resource specific validation and mutation handler
func ValidateConfiguration(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	var oldConfiguration *v1alpha1.Configuration
	if old != nil {
		var ok bool
		oldConfiguration, ok = old.(*v1alpha1.Configuration)
		if !ok {
			return fmt.Errorf("Failed to convert old into Configuration")
		}
	}
	glog.Infof("ValidateConfiguration: OLD Configuration is\n%+v", oldConfiguration)
	newConfiguration, ok := new.(*v1alpha1.Configuration)
	if !ok {
		return fmt.Errorf("Failed to convert new into Configuration")
	}
	glog.Infof("ValidateConfiguration: NEW Configuration is\n%+v", newConfiguration)

	if err := validateConfiguration(newConfiguration); err != nil {
		return err
	}
	return nil
}

func validateConfiguration(configuration *v1alpha1.Configuration) error {
	if reflect.DeepEqual(configuration.Spec, v1alpha1.ConfigurationSpec{}) {
		return fmt.Errorf(errEmptySpecInConfigurationMessage)
	}
	// TODO: add validation for configuration.Spec.Template, after we add a
	// validation for Revision.
	if reflect.DeepEqual(configuration.Spec.Template, v1alpha1.Revision{}) {
		return fmt.Errorf(errEmptyTemplateInSpecMessage)
	}
	if !reflect.DeepEqual(configuration.Status, v1alpha1.ConfigurationStatus{}) {
		return fmt.Errorf(errNonEmptyStatusInConfigurationMessage)
	}
	return nil
}
