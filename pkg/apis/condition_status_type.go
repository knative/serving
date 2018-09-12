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
	"reflect"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	ConditionTrue    = corev1.ConditionTrue
	ConditionFalse   = corev1.ConditionFalse
	ConditionUnknown = corev1.ConditionUnknown
)

// Conditions communicates the observed state of the Knative resource (from the controller).
type Conditions []Condition

// Conditions defines a readiness condition for a Knative resource.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
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
	LastTransitionTime VolatileTime `json:"lastTransitionTime,omitempty" description:"last time the condition transit from one status to another"`

	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

func (c *Condition) IsTrue() bool {
	return c.Status == ConditionTrue
}

// IsReady looks at all conditions in the required array.
// Returns true if all condition[requires..].Status are true.
func (cs Conditions) IsReady(requires []ConditionType) bool {
	for _, t := range requires {
		if c := cs.GetCondition(t); c == nil || !c.IsTrue() {
			return false
		}
	}
	return true
}

// GetCondition finds and returns the Condition that matches the ConditionType previously set on Conditions.
func (cs Conditions) GetCondition(t ConditionType) *Condition {
	for _, c := range cs {
		if c.Type == t {
			return &c
		}
	}
	return nil
}

// SetCondition sets or updates the Condition on Conditions for Condition.Type.
// returns the mutated list of Conditions.
func (cs Conditions) SetCondition(new *Condition) Conditions {
	if new == nil {
		return cs
	}
	t := new.Type
	var conditions []Condition
	for _, c := range cs {
		if c.Type != t {
			conditions = append(conditions, c)
		} else {
			// If we'd only update the LastTransitionTime, then return.
			new.LastTransitionTime = c.LastTransitionTime
			if reflect.DeepEqual(new, &c) {
				return cs
			}
		}
	}
	new.LastTransitionTime = VolatileTime{metav1.NewTime(time.Now())}
	conditions = append(conditions, *new)
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	return conditions
}

// MarkTrue sets the status of t to true.
// returns the mutated list of Conditions.
func (cs Conditions) MarkTrue(t ConditionType, top ConditionType, trumps []ConditionType) Conditions {
	for _, cond := range trumps {
		c := cs.GetCondition(cond)
		// Failed or Unknown conditions trump true conditions
		if c == nil || c.Status != corev1.ConditionTrue {
			return cs
		}
	} // TODO: this needs to flip and add TOP
	return cs.SetCondition(&Condition{
		Type:   t,
		Status: corev1.ConditionTrue,
	})
}

// MarkUnknown sets the status of t to true.
// returns the mutated list of Conditions.
func (cs Conditions) MarkUnknown(t ConditionType, reason, message string, top ConditionType, trumps []ConditionType) Conditions {
	for _, c := range trumps {
		c := cs.GetCondition(c)
		if c == nil || c.Status == corev1.ConditionFalse {
			// Failed conditions trump unknown conditions
			return cs
		}
	} // TODO: this needs to flip and add TOP
	return cs.SetCondition(&Condition{
		Type:    t,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
}

// MarkFalse sets the status of t to true.
// returns the mutated list of Conditions.
func (cs Conditions) MarkFalse(t ConditionType, reason, message string, top ConditionType) Conditions {
	for _, t := range []ConditionType{
		t,
		top,
	} {
		cs = cs.SetCondition(&Condition{
			Type:    t,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	}
	return cs
}

// InitializeConditions updates the Ready Condition to unknown if not set.
// returns the mutated list of Conditions.
func (cs Conditions) InitializeConditions(ts []ConditionType) Conditions {
	for _, t := range ts {
		if c := cs.GetCondition(t); c == nil {
			cs = cs.SetCondition(&Condition{
				Type:   t,
				Status: corev1.ConditionUnknown,
			})
		}
	}
	return cs
}
