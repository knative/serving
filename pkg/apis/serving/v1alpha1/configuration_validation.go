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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/equality"
)

func (c *Configuration) Validate() *FieldError {
	return c.Spec.Validate().ViaField("spec")
}

func (cs *ConfigurationSpec) Validate() *FieldError {
	if equality.Semantic.DeepEqual(cs, &ConfigurationSpec{}) {
		return errMissingField(currentField)
	}
	// In the context of Configuration, serving state may not be specified at all.
	// TODO(mattmoor): Check ObjectMeta for Name/Namespace/GenerateName
	if cs.RevisionTemplate.Spec.ServingState != "" {
		return errDisallowedFields("revisionTemplate.spec.servingState")
	}
	return cs.RevisionTemplate.Validate().ViaField("revisionTemplate")
}
