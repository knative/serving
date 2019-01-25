/*
Copyright 2018 The Knative Authors

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

package v1alpha1

import (
	"fmt"
	"time"

	"github.com/knative/pkg/apis"
)

// Validate Build
func (b *Build) Validate() *apis.FieldError {
	return validateObjectMetadata(b.GetObjectMeta()).ViaField("metadata").Also(b.Spec.Validate().ViaField("spec"))
}

// Validate for build spec
func (bs *BuildSpec) Validate() *apis.FieldError {
	if bs.Template == nil && len(bs.Steps) == 0 {
		return apis.ErrMissingField("b.spec.template").Also(apis.ErrMissingField("b.spec.steps"))
	}
	if bs.Template != nil && len(bs.Steps) > 0 {
		return apis.ErrMissingField("b.spec.template").Also(apis.ErrMissingField("b.spec.steps"))
	}

	if bs.Template != nil && bs.Template.Name == "" {
		apis.ErrMissingField("build.spec.template.name")
	}

	// If a build specifies a template, all the template's parameters without
	// defaults must be satisfied by the build's parameters.
	if bs.Template != nil {
		return bs.Template.Validate()
	}
	if err := ValidateVolumes(bs.Volumes); err != nil {
		return err
	}
	if err := bs.validateTimeout(); err != nil {
		return err
	}

	if err := validateSteps(bs.Steps); err != nil {
		return err
	}
	return nil
}

// Validate templateKind
func (b *TemplateInstantiationSpec) Validate() *apis.FieldError {
	if b == nil {
		return nil
	}
	if b.Name == "" {
		return apis.ErrMissingField("build.spec.template.name")
	}
	if b.Kind != "" {
		switch b.Kind {
		case ClusterBuildTemplateKind,
			BuildTemplateKind:
			return nil
		default:
			return apis.ErrInvalidValue(string(b.Kind), apis.CurrentField)
		}
	}
	return nil
}

func (bt *BuildSpec) validateTimeout() *apis.FieldError {
	if bt.Timeout == nil {
		return nil
	}
	maxTimeout := time.Duration(24 * time.Hour)

	if bt.Timeout.Duration > maxTimeout {
		return apis.ErrInvalidValue(fmt.Sprintf("%s should be < 24h", bt.Timeout), "b.spec.timeout")
	} else if bt.Timeout.Duration < 0 {
		return apis.ErrInvalidValue(fmt.Sprintf("%s should be > 0", bt.Timeout), "b.spec.timeout")
	}
	return nil
}
