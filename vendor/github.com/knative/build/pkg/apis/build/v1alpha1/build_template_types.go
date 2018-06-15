/*
Copyright 2018 Google, Inc. All rights reserved.

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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type BuildTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BuildTemplateSpec   `json:"spec"`
	Status BuildTemplateStatus `json:"status"`
}

type BuildTemplateSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// Parameters defines the parameters that can be populated in a template.
	Parameters []ParameterSpec    `json:"parameters,omitempty"`
	Steps      []corev1.Container `json:"steps"`
	Volumes    []corev1.Volume    `json:"volumes"`
}

// BuildTemplateStatus is the status for a Build resource
type BuildTemplateStatus struct {
	Conditions []BuildTemplateCondition `json:"conditions,omitempty"`
}

type BuildTemplateConditionType string

const (
	// BuildTemplateInvalid specifies that the given specification is invalid.
	//
	// TODO(jasonhall): Remove when webhook validation rejects invalid build templates.
	BuildTemplateInvalid BuildTemplateConditionType = "Invalid"
)

// BuildTemplateCondition defines a readiness condition for a BuildTemplate.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type BuildTemplateCondition struct {
	Type BuildTemplateConditionType `json:"state"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// ParameterSpec defines the possible parameters that can be populated in a
// template.
type ParameterSpec struct {
	// Name is the unique name of this template parameter.
	Name string `json:"name"`

	// Description is a human-readable explanation of this template parameter.
	Description string `json:"description,omitempty"`

	// Default, if specified, defines the default value that should be applied if
	// the build does not specify the value for this parameter.
	Default *string `json:"default,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BuildTemplateList is a list of BuildTemplate resources
type BuildTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []BuildTemplate `json:"items"`
}

func (b *BuildTemplateStatus) SetCondition(newCond *BuildTemplateCondition) {
	if newCond == nil {
		return
	}

	t := newCond.Type
	var conditions []BuildTemplateCondition
	for _, cond := range b.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	conditions = append(conditions, *newCond)
	b.Conditions = conditions
}

func (b *BuildTemplateStatus) RemoveCondition(t BuildTemplateConditionType) {
	var conditions []BuildTemplateCondition
	for _, cond := range b.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	b.Conditions = conditions
}

func (bt *BuildTemplate) GetGeneration() int64           { return bt.Spec.Generation }
func (bt *BuildTemplate) SetGeneration(generation int64) { bt.Spec.Generation = generation }
func (bt *BuildTemplate) GetSpecJSON() ([]byte, error)   { return json.Marshal(bt.Spec) }
