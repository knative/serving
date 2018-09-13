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

// +k8s:deepcopy-gen=true
type Conditions interface {
	GetConditions() []Condition
	SetConditions([]Condition)
}

func NewConditionSet(l ConditionType, d ...ConditionType) ConditionSet {
	c := ConditionSet{
		lead:       l,
		dependents: d,
	}
	return c
}

// ConditionSet holds the expected ConditionTypes to be checked.
// +k8s:deepcopy-gen=false
type ConditionSet struct {
	lead       ConditionType
	dependents []ConditionType
}

// ConditionsImpl implements the helper methods for evaluating Conditions.
// +k8s:deepcopy-gen=false
type ConditionsImpl struct {
	ConditionSet
	conditions Conditions
}

// Using creates a ConditionsImpl from an object that implements Conditions and
// the original ConditionSet.
func (r ConditionSet) Using(Conditions Conditions) ConditionsImpl {
	return ConditionsImpl{
		conditions: Conditions,
		ConditionSet: ConditionSet{
			lead:       r.lead,
			dependents: r.dependents,
		},
	}
}

// IsReady looks at the lead condition and returns true if that condition is
// set to true.
func (r ConditionsImpl) IsReady() bool {
	if c := r.GetCondition(r.lead); c == nil || !c.IsTrue() {
		return false
	}
	return true
}

// GetCondition finds and returns the Condition that matches the ConditionType
// previously set on Conditions.
func (r ConditionsImpl) GetCondition(t ConditionType) *Condition {
	if r.conditions == nil {
		return nil
	}
	for _, c := range r.conditions.GetConditions() {
		if c.Type == t {
			return &c
		}
	}
	return nil
}

// SetCondition sets or updates the Condition on Conditions for Condition.Type.
func (r ConditionsImpl) SetCondition(new *Condition) {
	if r.conditions == nil {
		return
	}
	if new == nil {
		return
	}
	t := new.Type
	var conditions []Condition
	for _, c := range r.conditions.GetConditions() {
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
	r.conditions.SetConditions(conditions)
}

// MarkTrue sets the status of t to true, and then marks the lead condition to
// true if all other dependents are also true.
func (r ConditionsImpl) MarkTrue(t ConditionType) {
	// set the specified condition
	r.SetCondition(&Condition{
		Type:   t,
		Status: corev1.ConditionTrue,
	})

	// check the dependents.
	for _, cond := range r.dependents {
		c := r.GetCondition(cond)
		// Failed or Unknown conditions trump true conditions
		if c == nil || c.Status != corev1.ConditionTrue {
			return
		}
	}

	// set the lead condition
	r.SetCondition(&Condition{
		Type:   r.lead,
		Status: corev1.ConditionTrue,
	})
}

// MarkUnknown sets the status of t to Unknown and also sets the lead condition
// to Unknown if no other dependent condition is in an error state.
func (r ConditionsImpl) MarkUnknown(t ConditionType, reason, message string) {
	// set the specified condition
	r.SetCondition(&Condition{
		Type:   t,
		Status: corev1.ConditionUnknown,
	})

	// check the dependents.
	for _, cond := range r.dependents {
		c := r.GetCondition(cond)
		// Failed conditions trump Unknown conditions
		if c == nil || c.Status == corev1.ConditionFalse {
			return
		}
	}

	// set the lead condition
	r.SetCondition(&Condition{
		Type:    r.lead,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
}

// MarkFalse sets the status of t and the lead condition to False.
func (r ConditionsImpl) MarkFalse(t ConditionType, reason, message string) {
	for _, t := range []ConditionType{
		t,
		r.lead,
	} {
		r.SetCondition(&Condition{
			Type:    t,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	}
}

// InitializeConditions updates all Conditions in the ConditionSet to Unknown
// if not set.
func (r ConditionsImpl) InitializeConditions() {
	for _, t := range append(r.dependents, r.lead) {
		r.SetCondition(&Condition{
			Type:   t,
			Status: corev1.ConditionUnknown,
		})
	}
}
