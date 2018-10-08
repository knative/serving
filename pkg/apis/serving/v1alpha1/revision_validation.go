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
	"strconv"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/knative/pkg/apis"
)

func (rt *Revision) Validate() *apis.FieldError {
	return ValidateObjectMetadata(rt.GetObjectMeta()).ViaField("metadata").
		Also(rt.Spec.Validate().ViaField("spec"))
}

func (rt *RevisionTemplateSpec) Validate() *apis.FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rs *RevisionSpec) Validate() *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &RevisionSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	errs := rs.ServingState.Validate().ViaField("servingState").
		Also(validateContainer(rs.Container).ViaField("container")).
		Also(validateBuildRef(rs.BuildRef).ViaField("buildRef"))

	if err := rs.ConcurrencyModel.Validate().ViaField("concurrencyModel"); err != nil {
		errs = errs.Also(err)
	} else if err := ValidateContainerConcurrency(rs.ContainerConcurrency, rs.ConcurrencyModel); err != nil {
		errs = errs.Also(err)
	}
	return errs
}

func (ss RevisionServingStateType) Validate() *apis.FieldError {
	switch ss {
	case RevisionServingStateType(""),
		RevisionServingStateRetired,
		RevisionServingStateReserve,
		RevisionServingStateActive:
		return nil
	default:
		return apis.ErrInvalidValue(string(ss), apis.CurrentField)
	}
}

func (cm RevisionRequestConcurrencyModelType) Validate() *apis.FieldError {
	switch cm {
	case RevisionRequestConcurrencyModelType(""),
		RevisionRequestConcurrencyModelMulti,
		RevisionRequestConcurrencyModelSingle:
		return nil
	default:
		return apis.ErrInvalidValue(string(cm), apis.CurrentField)
	}
}

func ValidateContainerConcurrency(cc RevisionContainerConcurrencyType, cm RevisionRequestConcurrencyModelType) *apis.FieldError {
	// Validate ContainerConcurrency alone
	if cc < 0 || cc > RevisionContainerConcurrencyMax {
		return apis.ErrInvalidValue(strconv.Itoa(int(cc)), "containerConcurrency")
	}

	// Validate combinations of ConcurrencyModel and ContainerConcurrency
	if cc == 0 && cm != RevisionRequestConcurrencyModelMulti && cm != RevisionRequestConcurrencyModelType("") {
		return apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel")
	}
	if cc == 1 && cm != RevisionRequestConcurrencyModelSingle && cm != RevisionRequestConcurrencyModelType("") {
		return apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel")
	}
	if cc > 1 && cm != RevisionRequestConcurrencyModelType("") {
		return apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel")
	}

	return nil
}

func validateContainer(container corev1.Container) *apis.FieldError {
	if equality.Semantic.DeepEqual(container, corev1.Container{}) {
		return apis.ErrMissingField(apis.CurrentField)
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
	var errs *apis.FieldError
	if len(ignoredFields) > 0 {
		// Complain about all ignored fields so that user can remove them all at once.
		errs = errs.Also(apis.ErrDisallowedFields(ignoredFields...))
	}
	// Validate our probes
	if err := validateProbe(container.ReadinessProbe).ViaField("readinessProbe"); err != nil {
		errs = errs.Also(err)
	}
	if err := validateProbe(container.LivenessProbe).ViaField("livenessProbe"); err != nil {
		errs = errs.Also(err)
	}
	return errs
}

func validateBuildRef(buildRef *corev1.ObjectReference) *apis.FieldError {
	if buildRef == nil {
		return nil
	}
	if len(validation.IsQualifiedName(buildRef.APIVersion)) != 0 {
		return apis.ErrInvalidValue(buildRef.APIVersion, "apiVersion")
	}
	if len(validation.IsCIdentifier(buildRef.Kind)) != 0 {
		return apis.ErrInvalidValue(buildRef.Kind, "kind")
	}
	if len(validation.IsDNS1123Label(buildRef.Name)) != 0 {
		return apis.ErrInvalidValue(buildRef.Name, "name")
	}
	var disallowedFields []string
	if buildRef.Namespace != "" {
		disallowedFields = append(disallowedFields, "namespace")
	}
	if buildRef.FieldPath != "" {
		disallowedFields = append(disallowedFields, "fieldPath")
	}
	if buildRef.ResourceVersion != "" {
		disallowedFields = append(disallowedFields, "resourceVersion")
	}
	if buildRef.UID != "" {
		disallowedFields = append(disallowedFields, "uid")
	}
	if len(disallowedFields) != 0 {
		return apis.ErrDisallowedFields(disallowedFields...)
	}
	return nil
}

func validateProbe(p *corev1.Probe) *apis.FieldError {
	if p == nil {
		return nil
	}
	emptyPort := intstr.IntOrString{}
	switch {
	case p.Handler.HTTPGet != nil:
		if p.Handler.HTTPGet.Port != emptyPort {
			return apis.ErrDisallowedFields("httpGet.port")
		}
	case p.Handler.TCPSocket != nil:
		if p.Handler.TCPSocket.Port != emptyPort {
			return apis.ErrDisallowedFields("tcpSocket.port")
		}
	}
	return nil
}

func (current *Revision) CheckImmutableFields(og apis.Immutable) *apis.FieldError {
	original, ok := og.(*Revision)
	if !ok {
		return &apis.FieldError{Message: "The provided original was not a Revision"}
	}

	// The autoscaler is allowed to change ServingState, but consider the rest.
	ignoreServingState := cmpopts.IgnoreFields(RevisionSpec{}, "ServingState")
	if diff := cmp.Diff(original.Spec, current.Spec, ignoreServingState); diff != "" {
		return &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: diff,
		}
	}
	return nil
}
