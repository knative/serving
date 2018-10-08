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
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/pkg/apis"
)

func (c *Configuration) Validate() *apis.FieldError {
	return ValidateObjectMetadata(c.GetObjectMeta()).ViaField("metadata").
		Also(c.Spec.Validate().ViaField("spec"))
}

func (cs *ConfigurationSpec) Validate() *apis.FieldError {
	if equality.Semantic.DeepEqual(cs, &ConfigurationSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var errs *apis.FieldError
	// In the context of Configuration, serving state may not be specified at all.
	// TODO(mattmoor): Check ObjectMeta for Name/Namespace/GenerateName
	if cs.RevisionTemplate.Spec.ServingState != "" {
		errs = apis.ErrDisallowedFields("revisionTemplate.spec.servingState")
	}

	if cs.Build == nil {
		// No build was specified.
	} else if err := cs.Build.As(&buildv1alpha1.BuildSpec{}); err == nil {
		// It is a BuildSpec, this is the legacy path.
	} else if err = cs.Build.As(&unstructured.Unstructured{}); err == nil {
		// It is an unstructured.Unstructured.
	} else {
		errs = errs.Also(apis.ErrInvalidValue(err.Error(), "build"))
	}

	return errs.Also(cs.RevisionTemplate.Validate().ViaField("revisionTemplate"))
}
