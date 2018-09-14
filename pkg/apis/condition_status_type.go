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

package apis

import (
	"github.com/knative/pkg/apis"
	corev1 "k8s.io/api/core/v1"
)

type ConditionType string

const (
	// ConditionReady specifies that the resource is ready.
	// For long-running resources.
	ConditionReady ConditionType = "Ready"
	// ConditionSucceeded specifies that the resource has finished.
	// For resource which run to completion.
	ConditionSucceeded ConditionType = "Succeeded"
)

// Conditions defines a readiness condition for a Knative resource.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
// +k8s:deepcopy-gen=true
type Condition struct {
	// Type of condition.
	// +required
	Type ConditionType `json:"type" description:"type of status condition"`

	// Status of the condition, one of True, False, Unknown.
	// +required
	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// LastTransitionTime is the last time the condition transitioned from one status to another.
	// We use VolatileTime in place of metav1.Time to exclude this from creating equality.Semantic
	// differences (all other things held constant).
	// +optional
	LastTransitionTime apis.VolatileTime `json:"lastTransitionTime,omitempty" description:"last time the condition transit from one status to another"`

	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// IsTrue is true if the condition is True
func (c *Condition) IsTrue() bool {
	return c.Status == corev1.ConditionTrue
}

// IsFalse is true if the condition is False
func (c *Condition) IsFalse() bool {
	return c.Status == corev1.ConditionFalse
}

// IsUnknown is true if the condition is Unknown
func (c *Condition) IsUnknown() bool {
	return c.Status == corev1.ConditionUnknown
}
