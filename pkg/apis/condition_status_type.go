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

	"github.com/knative/pkg/apis"
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
	LastTransitionTime apis.VolatileTime `json:"lastTransitionTime,omitempty" description:"last time the condition transit from one status to another"`

	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

type Conditional interface {
	GetConditions() []Condition
	SetConditions([]Condition)
}

func (c *Condition) IsTrue() bool {
	return c.Status == ConditionTrue
}

func NewConditioner(l ConditionType, d ...ConditionType) *Conditioner {
	c := &Conditioner{
		lead:       l,
		dependents: d,
	}
	return c
}

type Conditioner struct {
	lead       ConditionType
	dependents []ConditionType
}

type Conditions struct {
	Conditions []Condition
}

// IsReady looks at all conditions in the required array.
// Returns true if all condition[requires..].Status are true.
func (r *Conditioner) IsReady(conditional Conditional) bool {
	for _, t := range r.dependents {
		if c := r.GetCondition(t, conditional); c == nil || !c.IsTrue() {
			return false
		}
	}
	return true
}

// GetCondition finds and returns the Condition that matches the ConditionType previously set on Conditions.
func (r *Conditioner) GetCondition(t ConditionType, conditional Conditional) *Condition {
	for _, c := range conditional.GetConditions() {
		if c.Type == t {
			return &c
		}
	}
	return nil
}

// SetCondition sets or updates the Condition on Conditions for Condition.Type.
// returns the mutated list of Conditions.
func (r *Conditioner) SetCondition(new *Condition, conditional Conditional) {
	if new == nil {
		return
	}
	t := new.Type
	var conditions []Condition
	for _, c := range conditional.GetConditions() {
		if c.Type != t {
			conditions = append(conditions, c)
		} else {
			// If we'd only update the LastTransitionTime, then return.
			new.LastTransitionTime = c.LastTransitionTime
			if reflect.DeepEqual(new, &c) {
				return
			}
		}
	}
	new.LastTransitionTime = apis.VolatileTime{metav1.NewTime(time.Now())}
	conditions = append(conditions, *new)
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	conditional.SetConditions(conditions)
}

// MarkTrue sets the status of t to true.
// returns the mutated list of Conditions.
func (r *Conditioner) MarkTrue(t ConditionType, conditional Conditional) {
	// set the specified condition
	r.SetCondition(&Condition{
		Type:   t,
		Status: ConditionTrue,
	}, conditional)

	// check the dependents.
	for _, cond := range r.dependents {
		c := r.GetCondition(cond, conditional)
		// Failed or Unknown conditions trump true conditions
		if c == nil || c.Status != ConditionTrue {
			return
		}
	}

	// set the lead condition
	r.SetCondition(&Condition{
		Type:   r.lead,
		Status: ConditionTrue,
	}, conditional)
}

// MarkUnknown sets the status of t to true.
// returns the mutated list of Conditions.
func (r *Conditioner) MarkUnknown(t ConditionType, reason, message string, conditional Conditional) {
	// set the specified condition
	r.SetCondition(&Condition{
		Type:   t,
		Status: ConditionUnknown,
	}, conditional)

	// check the dependents.
	for _, cond := range r.dependents {
		c := r.GetCondition(cond, conditional)
		// Failed conditions trump unknown conditions
		if c == nil || c.Status == ConditionFalse {
			return
		}
	}

	// set the lead condition
	r.SetCondition(&Condition{
		Type:    r.lead,
		Status:  ConditionUnknown,
		Reason:  reason,
		Message: message,
	}, conditional)
}

// MarkFalse sets the status of t to true.
// returns the mutated list of Conditions.
func (r *Conditioner) MarkFalse(t ConditionType, reason, message string, conditional Conditional) {
	for _, t := range []ConditionType{
		t,
		r.lead,
	} {
		r.SetCondition(&Condition{
			Type:    t,
			Status:  ConditionFalse,
			Reason:  reason,
			Message: message,
		}, conditional)
	}
}

// InitializeConditions updates the Ready Condition to unknown if not set.
// returns the mutated list of Conditions.
func (r *Conditioner) InitializeConditions(conditional Conditional) {
	if c := r.GetCondition(r.lead, conditional); c == nil {
		r.SetCondition(&Condition{
			Type:   r.lead,
			Status: ConditionUnknown,
		}, conditional)
	}
}
