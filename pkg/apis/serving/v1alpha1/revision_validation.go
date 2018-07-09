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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

func (rt *Revision) Validate() *FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rt *RevisionTemplateSpec) Validate() *FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rs *RevisionSpec) Validate() *FieldError {
	if equality.Semantic.DeepEqual(rs, &RevisionSpec{}) {
		return errMissingField(currentField)
	}
	if err := rs.ServingState.Validate(); err != nil {
		return err.ViaField("servingState")
	}
	if err := validateContainer(rs.Container); err != nil {
		return err.ViaField("container")
	}
	return rs.ConcurrencyModel.Validate().ViaField("concurrencyModel")
}

func (ss RevisionServingStateType) Validate() *FieldError {
	switch ss {
	case RevisionServingStateType(""),
		RevisionServingStateRetired,
		RevisionServingStateReserve,
		RevisionServingStateActive:
		return nil
	default:
		return errInvalidValue(string(ss), currentField)
	}
}

func (cm RevisionRequestConcurrencyModelType) Validate() *FieldError {
	switch cm {
	case RevisionRequestConcurrencyModelType(""),
		RevisionRequestConcurrencyModelMulti,
		RevisionRequestConcurrencyModelSingle:
		return nil
	default:
		return errInvalidValue(string(cm), currentField)
	}
}

func validateContainer(container corev1.Container) *FieldError {
	if equality.Semantic.DeepEqual(container, corev1.Container{}) {
		return errMissingField(currentField)
	}
	// Some corev1.Container fields are set by Knative Serving controller.  We disallow them
	// here to avoid silently overwriting these fields and causing confusions for
	// the users.  See pkg/controller/revision/resources/deploy.go#makePodSpec.
	var ignoredFields []string
	if container.Name != "" {
		ignoredFields = append(ignoredFields, "name")
	}
	if !equality.Semantic.DeepEqual(container.Resources, corev1.ResourceRequirements{}) {
		ignoredFields = append(ignoredFields, "resources")
	}
	if len(container.Ports) > 0 {
		ignoredFields = append(ignoredFields, "ports")
	}
	if len(container.VolumeMounts) > 0 {
		ignoredFields = append(ignoredFields, "volumeMounts")
	}
	if container.Lifecycle != nil {
		ignoredFields = append(ignoredFields, "lifecycle")
	}
	if len(ignoredFields) > 0 {
		// Complain about all ignored fields so that user can remove them all at once.
		return errDisallowedFields(ignoredFields...)
	}
	return nil
}
