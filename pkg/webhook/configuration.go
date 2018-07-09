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
	"path"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidConfigurationInput = errors.New("failed to convert input into Configuration")
)

// ValidateConfiguration is Configuration resource specific validation and mutation handler
func ValidateConfiguration(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		_, newConfiguration, err := unmarshalConfigurations(ctx, old, new, "ValidateConfiguration")
		if err != nil {
			return err
		}

		// Can't just `return newConfiguration.Validate()` because it doesn't properly nil-check.
		if err := newConfiguration.Validate(); err != nil {
			return err
		}
		return nil
	}
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
