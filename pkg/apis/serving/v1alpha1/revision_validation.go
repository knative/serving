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
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/knative/pkg/apis"
)

func (rt *Revision) Validate() *apis.FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rt *RevisionTemplateSpec) Validate() *apis.FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rs *RevisionSpec) Validate() *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &RevisionSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	if err := rs.ServingState.Validate(); err != nil {
		return err.ViaField("servingState")
	}
	if err := validateContainer(rs.Container); err != nil {
		return err.ViaField("container")
	}
	if err := validateNodeSelector(rs.NodeSelector); err != nil {
		return err.ViaField("nodeSelector")
	}
	for i, toleration := range rs.Tolerations {
		if err := validateToleration(toleration); err != nil {
			return err.ViaField(
				fmt.Sprintf("tolerations[%d]", i),
			)
		}
	}
	return rs.ConcurrencyModel.Validate().ViaField("concurrencyModel")
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
	if len(ignoredFields) > 0 {
		// Complain about all ignored fields so that user can remove them all at once.
		return apis.ErrDisallowedFields(ignoredFields...)
	}
	// Validate our probes
	if err := validateProbe(container.ReadinessProbe); err != nil {
		return err.ViaField("readinessProbe")
	}
	if err := validateProbe(container.LivenessProbe); err != nil {
		return err.ViaField("livenessProbe")
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

func validateNodeSelector(nodeSelector map[string]string) *apis.FieldError {
	for key, value := range nodeSelector {
		if errStrs :=
			validation.IsQualifiedName(key); len(errStrs) > 0 {
			return apis.ErrInvalidKeyName(
				key,
				key,
				strings.Join(errStrs, "; "),
			)
		}
		if errStrs :=
			validation.IsValidLabelValue(value); len(errStrs) != 0 {
			return &apis.FieldError{
				Message: fmt.Sprintf("invalid value %q", value),
				Details: strings.Join(errStrs, "; "),
				Paths:   []string{key},
			}
		}
	}
	return nil
}

// validateToleration duplicates and adapts logic from
// k8s.io/kubernetes/pkg/apis/core/validation. Although relevant functions from
// that package are exported, they're not usable here because they are for
// unversioned resources.
func validateToleration(toleration corev1.Toleration) *apis.FieldError {
	if toleration.Key != "" {
		if errStrs :=
			validation.IsQualifiedName(toleration.Key); len(errStrs) > 0 {
			return &apis.FieldError{
				Message: fmt.Sprintf("invalid value %q", toleration.Key),
				Details: strings.Join(errStrs, "; "),
				Paths:   []string{"key"},
			}
		}
	}
	// empty toleration key with Exists operator means match all taints
	if toleration.Key == "" &&
		toleration.Operator != corev1.TolerationOpExists {
		return &apis.FieldError{
			Message: fmt.Sprintf("invalid value %q", toleration.Operator),
			Details: "operator must be Exists when `key` is empty, which means " +
				`"match all values and all keys"`,
			Paths: []string{"operator"},
		}
	}
	if toleration.TolerationSeconds != nil &&
		toleration.Effect != corev1.TaintEffectNoExecute {
		return &apis.FieldError{
			Message: fmt.Sprintf("invalid value %q", toleration.Effect),
			Details: "effect must be 'NoExecute' when `tolerationSeconds` is set",
			Paths:   []string{"effect"},
		}
	}
	// validate toleration operator and value
	switch toleration.Operator {
	// empty operator means Equal
	case corev1.TolerationOpEqual, "":
		if errStrs :=
			validation.IsValidLabelValue(toleration.Value); len(errStrs) != 0 {
			return &apis.FieldError{
				Message: fmt.Sprintf("invalid value %q", toleration.Value),
				Details: strings.Join(errStrs, "; "),
				Paths:   []string{"value"},
			}
		}
	case corev1.TolerationOpExists:
		if toleration.Value != "" {
			return &apis.FieldError{
				Message: fmt.Sprintf("invalid value %q", toleration.Value),
				Details: "value must be empty when `operator` is 'Exists'",
				Paths:   []string{"value"},
			}
		}
	default:
		validValues := []string{
			string(corev1.TolerationOpEqual),
			string(corev1.TolerationOpExists),
		}
		return &apis.FieldError{
			Message: fmt.Sprintf("invalid value %q", toleration.Operator),
			Details: fmt.Sprintf(
				"allowed values are: %s",
				strings.Join(validValues, ", "),
			),
			Paths: []string{"operator"},
		}
	}
	// validate toleration effect, empty toleration effect means match all taint
	// effects
	switch toleration.Effect {
	// TODO: Uncomment TaintEffectNoScheduleNoAdmit once it is implemented.
	case "", corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule,
		corev1.TaintEffectNoExecute: // corev1.TaintEffectNoScheduleNoAdmit
		return nil
	default:
		validValues := []string{
			string(corev1.TaintEffectNoSchedule),
			string(corev1.TaintEffectPreferNoSchedule),
			string(corev1.TaintEffectNoExecute),
			// TODO: Uncomment next line when TaintEffectNoScheduleNoAdmit is
			// implemented.
			// string(corev1.TaintEffectNoScheduleNoAdmit),
		}
		return &apis.FieldError{
			Message: fmt.Sprintf("invalid value %q", toleration.Effect),
			Details: fmt.Sprintf(
				"allowed values are: %s",
				strings.Join(validValues, ", "),
			),
			Paths: []string{"effect"},
		}
	}
	return nil
}
