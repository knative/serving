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
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/mattbaird/jsonpatch"
	corev1 "k8s.io/api/core/v1"
)

func errMissingField(fieldPath string) error {
	return fmt.Errorf("Configuration is missing %q", fieldPath)
}

func errDisallowedFields(fieldPaths string) error {
	return fmt.Errorf("The configuration spec must not set the field(s): %s", fieldPaths)
}

var (
	errEmptySpecInConfiguration         = errMissingField("spec")
	errEmptyRevisionTemplateInSpec      = errMissingField("spec.revisionTemplate")
	errEmptyContainerInRevisionTemplate = errMissingField("spec.revisionTemplate.spec.container")
	errInvalidConfigurationInput        = errors.New("failed to convert input into Configuration")
)

// ValidateConfiguration is Configuration resource specific validation and mutation handler
func ValidateConfiguration(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		_, newConfiguration, err := unmarshalConfigurations(ctx, old, new, "ValidateConfiguration")
		if err != nil {
			return err
		}
		if err := validateConfiguration(newConfiguration); err != nil {
			return err
		}
		return nil
	}
}

func validateConfiguration(configuration *v1alpha1.Configuration) error {
	if reflect.DeepEqual(configuration.Spec, v1alpha1.ConfigurationSpec{}) {
		return errEmptySpecInConfiguration
	}
	if err := validateConfigurationSpec(&configuration.Spec); err != nil {
		return err
	}
	return validateConcurrencyModel(configuration.Spec.RevisionTemplate.Spec.ConcurrencyModel)
}

func validateConfigurationSpec(configurationSpec *v1alpha1.ConfigurationSpec) error {
	if err := validateTemplate(&configurationSpec.RevisionTemplate); err != nil {
		return err
	}
	return nil
}

func validateTemplate(template *v1alpha1.RevisionTemplateSpec) error {
	if reflect.DeepEqual(*template, v1alpha1.RevisionTemplateSpec{}) {
		return errEmptyRevisionTemplateInSpec
	}
	if template.Spec.ServingState != "" {
		return errDisallowedFields("revisionTemplate.spec.servingState")
	}
	if err := validateContainer(template.Spec.Container); err != nil {
		return err
	}
	return nil
}

func validateConcurrencyModel(value v1alpha1.RevisionRequestConcurrencyModelType) error {
	switch value {
	case v1alpha1.RevisionRequestConcurrencyModelType(""), v1alpha1.RevisionRequestConcurrencyModelMulti, v1alpha1.RevisionRequestConcurrencyModelSingle:
		return nil
	default:
		return fmt.Errorf("Unrecognized value for concurrencyModel: %q", value)
	}
}

func validateContainer(container corev1.Container) error {
	if reflect.DeepEqual(container, corev1.Container{}) {
		return errEmptyContainerInRevisionTemplate
	}
	// Some corev1.Container fields are set by Knative Serving controller.  We disallow them
	// here to avoid silently overwriting these fields and causing confusions for
	// the users.  See pkg/controller/revision.MakeElaPodSpec for the list of fields
	// overridden.
	var ignoredFields []string
	if container.Name != "" {
		ignoredFields = append(ignoredFields, "revisionTemplate.spec.container.name")
	}
	if !reflect.DeepEqual(container.Resources, corev1.ResourceRequirements{}) {
		ignoredFields = append(ignoredFields, "revisionTemplate.spec.container.resources")
	}
	if len(container.Ports) > 0 {
		ignoredFields = append(ignoredFields, "revisionTemplate.spec.container.ports")
	}
	if len(container.VolumeMounts) > 0 {
		ignoredFields = append(ignoredFields, "revisionTemplate.spec.container.volumeMounts")
	}
	if container.Lifecycle != nil {
		ignoredFields = append(ignoredFields, "revisionTemplate.spec.container.lifecycle")
	}
	if len(ignoredFields) > 0 {
		// Complain about all ignored fields so that user can remove them all at once.
		return errDisallowedFields(strings.Join(ignoredFields, ", "))
	}
	return nil
}

// SetConfigurationDefaults set defaults on an configurations.
func SetConfigurationDefaults(ctx context.Context) ResourceDefaulter {
	return func(patches *[]jsonpatch.JsonPatchOperation, crd GenericCRD) error {
		_, config, err := unmarshalConfigurations(ctx, nil, crd, "SetConfigurationDefaults")
		if err != nil {
			return err
		}

		return setConfigurationSpecDefaults(patches, "/spec", config.Spec)
	}
}

func setConfigurationSpecDefaults(patches *[]jsonpatch.JsonPatchOperation, patchBase string, spec v1alpha1.ConfigurationSpec) error {
	if spec.RevisionTemplate.Spec.ConcurrencyModel == "" {
		*patches = append(*patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      path.Join(patchBase, "revisionTemplate/spec/concurrencyModel"),
			Value:     v1alpha1.RevisionRequestConcurrencyModelMulti,
		})
	}
	return nil
}

func unmarshalConfigurations(
	ctx context.Context, old GenericCRD, new GenericCRD, fnName string) (*v1alpha1.Configuration, *v1alpha1.Configuration, error) {
	logger := logging.FromContext(ctx)
	var oldConfiguration *v1alpha1.Configuration
	if old != nil {
		var ok bool
		oldConfiguration, ok = old.(*v1alpha1.Configuration)
		if !ok {
			return nil, nil, errInvalidConfigurationInput
		}
	}
	logger.Infof("%s: OLD Configuration is\n%+v", fnName, oldConfiguration)

	newConfiguration, ok := new.(*v1alpha1.Configuration)
	if !ok {
		return nil, nil, errInvalidConfigurationInput
	}
	logger.Infof("%s: NEW Configuration is\n%+v", fnName, newConfiguration)

	return oldConfiguration, newConfiguration, nil
}
