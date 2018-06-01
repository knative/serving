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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Service
type Service struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceSpec   `json:"spec,omitempty"`
	Status ServiceStatus `json:"status,omitempty"`
}

// ServiceSpec represents the configuration for the Service object.
// Exactly one of its members (other than Generation) must be specified.
type ServiceSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64 `json:"generation,omitempty"`

	// RunLatest defines a simple Service. It will automatically
	// configure a route that keeps the latest ready revision
	// from the supplied configuration running.
	// +optional
	RunLatest *RunLatestType `json:"runLatest,omitempty"`

	// Pins this service to a specific revision name. The revision must
	// be owned by the configuration provided.
	// +optional
	Pinned *PinnedType `json:"pinned,omitempty"`
}

type RunLatestType struct {
	// The configuration for this service.
	Configuration ConfigurationSpec `json:"configuration,omitempty"`
}

type PinnedType struct {
	// The revision name to pin this service to until changed
	// to a different service type.
	RevisionName string `json:"revisionName,omitempty"`

	// The configuration for this service.
	Configuration ConfigurationSpec `json:"configuration,omitempty"`
}

type ServiceCondition struct {
	Type ServiceConditionType `json:"type"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// ServiceConditionType represents an Service condition value
type ServiceConditionType string

const (
	// ServiceConditionReady is set when the service is configured
	// and has available backends ready to receive traffic.
	ServiceConditionReady ServiceConditionType = "Ready"
)

type ServiceStatus struct {
	Conditions []ServiceCondition `json:"conditions,omitempty"`

	// ObservedGeneration is the 'Generation' of the Service that
	// was last processed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceList is a list of Service resources
type ServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Service `json:"items"`
}

func (s *Service) GetGeneration() int64 {
	return s.Spec.Generation
}

func (s *Service) SetGeneration(generation int64) {
	s.Spec.Generation = generation
}

func (s *Service) GetSpecJSON() ([]byte, error) {
	return json.Marshal(s.Spec)
}

func (ss *ServiceStatus) IsReady() bool {
	if c := ss.GetCondition(ServiceConditionReady); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

func (ss *ServiceStatus) GetCondition(t ServiceConditionType) *ServiceCondition {
	for _, cond := range ss.Conditions {
		if cond.Type == t {
			return &cond
		}
	}
	return nil
}

func (ss *ServiceStatus) SetCondition(new *ServiceCondition) {
	if new == nil {
		return
	}

	t := new.Type
	var conditions []ServiceCondition
	for _, cond := range ss.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	conditions = append(conditions, *new)
	ss.Conditions = conditions
}

func (ss *ServiceStatus) RemoveCondition(t ServiceConditionType) {
	var conditions []ServiceCondition
	for _, cond := range ss.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	ss.Conditions = conditions
}
