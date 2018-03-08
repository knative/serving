/*
Copyright 2018 Google LLC.

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

	build "github.com/elafros/elafros/pkg/apis/cloudbuild/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Configuration
type Configuration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigurationSpec   `json:"spec,omitempty"`
	Status ConfigurationStatus `json:"status,omitempty"`
}

// ConfigurationSpec defines the desired state of Configuration
type ConfigurationSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64            `json:"generation,omitempty"`
	Build      *build.BuildSpec `json:"build,omitempty"`
	Template   Revision         `json:"template"`
}

// ConfigurationConditionType represents a Configuration condition value
type ConfigurationConditionType string

const (
	// ConfigurationConditionReady is set when the configuration is starting to materialize
	// runtime resources, and becomes true when those resources are ready.
	ConfigurationConditionReady ConfigurationConditionType = "Ready"
)

// ConfigurationCondition defines a readiness condition for a Configuration.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type ConfigurationCondition struct {
	Type ConfigurationConditionType `json:"type" description:"type of Configuration condition"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// ConfigurationStatus defines the observed state of Configuration
type ConfigurationStatus struct {
	Conditions []ConfigurationCondition `json:"conditions,omitempty"`

	// Latest revision that is ready.
	LatestReadyRevisionName string `json:"latestReadyRevisionName,omitempty"`

	// LatestCreatedRevisionName is the last revision that was created; it might not be
	// ready yet. When it's ready, it will get moved to LatestReady.
	LatestCreatedRevisionName string `json:"latestCreatedRevisionName,omitempty"`

	// ObservedGeneration is the 'Generation' of the Configuration that
	// was last processed by the controller. The observed generation is updated
	// even if the controller failed to process the spec and create the Revision.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ConfigurationList is a list of Configuration resources
type ConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Configuration `json:"items"`
}

func (r *Configuration) GetGeneration() int64 {
	return r.Spec.Generation
}

func (r *Configuration) SetGeneration(generation int64) {
	r.Spec.Generation = generation
}

func (r *Configuration) GetSpecJSON() ([]byte, error) {
	return json.Marshal(r.Spec)
}

// IsReady looks at the conditions on the ConfigurationStatus.
// ConfigurationConditionReady returns true if ConditionStatus is True
func (configStatus *ConfigurationStatus) IsReady() bool {
	if c := configStatus.GetCondition(ConfigurationConditionReady); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

func (config *ConfigurationStatus) GetCondition(t ConfigurationConditionType) *ConfigurationCondition {
	for _, cond := range config.Conditions {
		if cond.Type == t {
			return &cond
		}
	}
	return nil
}

func (configStatus *ConfigurationStatus) SetCondition(new *ConfigurationCondition) {
	if new == nil {
		return
	}

	t := new.Type
	var conditions []ConfigurationCondition
	for _, cond := range configStatus.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	conditions = append(conditions, *new)
	configStatus.Conditions = conditions
}

func (configStatus *ConfigurationStatus) RemoveCondition(t ConfigurationConditionType) {
	var conditions []ConfigurationCondition
	for _, cond := range configStatus.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	configStatus.Conditions = conditions
}
