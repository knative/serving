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
	"reflect"
	"strings"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/golang/glog"
	"github.com/mattbaird/jsonpatch"
	corev1 "k8s.io/api/core/v1"
)

var (
	errEmptyContainerSpecInRevisionTemplate = errors.New("The configuration template must have a container spec")
	errEmptySpecInConfiguration             = errors.New("The configuration must have configuration spec")
	errEmptyRevisionTemplateInSpec          = errors.New("The configuration spec must have configuration")
	errInvalidConfigurationInput            = errors.New("Failed to convert input into configuration")
)

// ValidateConfiguration is Configuration resource specific validation and mutation handler
func ValidateConfiguration(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	var oldConfiguration *v1alpha1.Configuration
	if old != nil {
		var ok bool
		oldConfiguration, ok = old.(*v1alpha1.Configuration)
		if !ok {
			return errInvalidConfigurationInput
		}
	}
	glog.Infof("ValidateConfiguration: OLD Configuration is\n%+v", oldConfiguration)
	newConfiguration, ok := new.(*v1alpha1.Configuration)
	if !ok {
		return errInvalidConfigurationInput
	}
	glog.Infof("ValidateConfiguration: NEW Configuration is\n%+v", newConfiguration)

	if err := validateConfiguration(newConfiguration); err != nil {
		return err
	}
	return nil
}

func validateConfiguration(configuration *v1alpha1.Configuration) error {
	if reflect.DeepEqual(configuration.Spec, v1alpha1.ConfigurationSpec{}) {
		return errEmptySpecInConfiguration
	}
	if err := validateTemplate(&configuration.Spec.RevisionTemplate); err != nil {
		return err
	}
	return nil
}

func validateTemplate(template *v1alpha1.Revision) error {
	if reflect.DeepEqual(*template, v1alpha1.Revision{}) {
		return errEmptyRevisionTemplateInSpec
	}
	if err := validateContainerSpec(template.Spec.ContainerSpec); err != nil {
		return err
	}
	return nil
}

func validateContainerSpec(container *corev1.Container) error {
	if container == nil || reflect.DeepEqual(*container, corev1.Container{}) {
		return errEmptyContainerSpecInRevisionTemplate
	}
	// Some corev1.Container fields are set by Elafros controller.  We disallow them
	// here to avoid silently overwriting these fields and causing confusions for
	// the users.  See pkg/controller/revision.MakeElaPodSpec for the list of fields
	// overridden.
	var ignoredFields []string
	if container.Name != "" {
		ignoredFields = append(ignoredFields, "revisiontemplate.spec.containerSpec.name")
	}
	if !reflect.DeepEqual(container.Resources, corev1.ResourceRequirements{}) {
		ignoredFields = append(ignoredFields, "revisiontemplate.spec.containerSpec.resources")
	}
	if len(container.Ports) > 0 {
		ignoredFields = append(ignoredFields, "revisiontemplate.spec.containerSpec.ports")
	}
	if len(container.VolumeMounts) > 0 {
		ignoredFields = append(ignoredFields, "revisiontemplate.spec.containerSpec.volumeMounts")
	}
	if len(ignoredFields) > 0 {
		// Complain about all ignored fields so that user can remove them all at once.
		return fmt.Errorf("The configuration spec must not set the field(s) %s", strings.Join(ignoredFields, ", "))
	}
	return nil
}
