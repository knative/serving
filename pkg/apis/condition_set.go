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

// Conditions is the interface for a Resource that implements the getter and
// setter for accessing a Condition collection.
// +k8s:deepcopy-gen=true
type Conditions interface {
	GetConditions() []Condition
	SetConditions([]Condition)
}

// ConditionManager allows a resource to operate on it's Conditions using higher
// order operations.
type ConditionManager interface {
	// IsHappy looks at the lead condition and returns true if that condition is
	// set to true.
	IsHappy() bool

	// GetCondition finds and returns the Condition that matches the ConditionType
	// previously set on Conditions.
	GetCondition(t ConditionType) *Condition

	// SetCondition sets or updates the Condition on Conditions for Condition.Type.
	// If there is an update, Conditions are stored back sorted.
	SetCondition(new Condition)

	// MarkTrue sets the status of t to true, and then marks the lead condition to
	// true if all other dependents are also true.
	MarkTrue(t ConditionType)

	// MarkUnknown sets the status of t to Unknown and also sets the lead condition
	// to Unknown if no other dependent condition is in an error state.
	MarkUnknown(t ConditionType, reason, message string)

	// MarkFalse sets the status of t and the lead condition to False.
	MarkFalse(t ConditionType, reason, message string)

	// InitializeConditions updates all Conditions in the ConditionSet to Unknown
	// if not set.
	InitializeConditions()
<<<<<<< HEAD

	// InitializeCondition updates a Condition to Unknown if not set.
	InitializeCondition(t ConditionType)
=======
>>>>>>> conditionals
}

// ConditionSet holds the expected ConditionTypes to be checked.
// +k8s:deepcopy-gen=false
type ConditionSet struct {
<<<<<<< HEAD
	lead                 ConditionType
	dependents           []ConditionType
	dependentsForInit    []ConditionType
	dependentsForTrue    []ConditionType
	dependentsForUnknown []ConditionType
=======
	lead       ConditionType
	dependents []ConditionType
>>>>>>> conditionals
}

// NewConditionSet returns a ConditionSet to hold the conditions that are
// important for the caller. The first ConditionType is the overarching status
// for that will be used to signal the resources' status is Ready or Succeeded.
func NewConditionSet(l ConditionType, d ...ConditionType) ConditionSet {
	c := ConditionSet{
<<<<<<< HEAD
		lead:                 l,
		dependents:           d,
		dependentsForInit:    d,
		dependentsForTrue:    d,
		dependentsForUnknown: d,
=======
		lead:       l,
		dependents: d,
>>>>>>> conditionals
	}
	return c
}

// NewOngoingConditionSet returns a ConditionSet to hold the conditions for the
// ongoing resource. ConditionReady is used as the lead.
func NewOngoingConditionSet(d ...ConditionType) ConditionSet {
<<<<<<< HEAD
	return NewConditionSet(ConditionReady, d...)
=======
	return ConditionSet{
		lead:       ConditionReady,
		dependents: d,
	}
>>>>>>> conditionals
}

// NewOngoingConditionSet returns a ConditionSet to hold the conditions for the
// run once resource. ConditionSucceeded is used as the lead.
func NewRunOnceConditionSet(d ...ConditionType) ConditionSet {
<<<<<<< HEAD
	return NewConditionSet(ConditionSucceeded, d...)
=======
	return ConditionSet{
		lead:       ConditionSucceeded,
		dependents: d,
	}
>>>>>>> conditionals
}

// Check that conditionsImpl implements ConditionManager.
var _ ConditionManager = conditionsImpl{}

// conditionsImpl implements the helper methods for evaluating Conditions.
// +k8s:deepcopy-gen=false
type conditionsImpl struct {
	ConditionSet
	conditions Conditions
}

// Using creates a conditionsImpl from an object that implements Conditions and
// the original ConditionSet.
func (r ConditionSet) Using(Conditions Conditions) ConditionManager {
	return conditionsImpl{
<<<<<<< HEAD
		conditions:   Conditions,
		ConditionSet: r,
	}
}

func (r ConditionSet) SetInits(t ...ConditionType) {
	r.dependentsForInit = t
}

func (r ConditionSet) SetOverallTrueRequires(t ...ConditionType) {
	r.dependentsForTrue = t
}

func (r ConditionSet) SetOverallUnknownRequires(t ...ConditionType) {
	r.dependentsForUnknown = t
}

func (r ConditionSet) getInitDependents() []ConditionType {
	return r.dependentsForInit
}

func (r ConditionSet) getMarkTrueDependents() []ConditionType {
	return r.dependentsForTrue
}

func (r ConditionSet) getMarkUnknownDependents() []ConditionType {
	return r.dependentsForUnknown
}

=======
		conditions: Conditions,
		ConditionSet: ConditionSet{
			lead:       r.lead,
			dependents: r.dependents,
		},
	}
}

>>>>>>> conditionals
// IsHappy looks at the lead condition and returns true if that condition is
// set to true.
func (r conditionsImpl) IsHappy() bool {
	if c := r.GetCondition(r.lead); c == nil || !c.IsTrue() {
		return false
	}
	return true
}

// GetCondition finds and returns the Condition that matches the ConditionType
// previously set on Conditions.
func (r conditionsImpl) GetCondition(t ConditionType) *Condition {
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
// If there is an update, Conditions are stored back sorted.
func (r conditionsImpl) SetCondition(new Condition) {
	if r.conditions == nil {
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
	conditions = append(conditions, new)
	// Sorted for convince of the consumer, i.e.: kubectl.
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	r.conditions.SetConditions(conditions)
}

// MarkTrue sets the status of t to true, and then marks the lead condition to
// true if all other dependents are also true.
func (r conditionsImpl) MarkTrue(t ConditionType) {
	// set the specified condition
	r.SetCondition(Condition{
		Type:   t,
		Status: corev1.ConditionTrue,
	})

	// check the dependents.
<<<<<<< HEAD
	for _, cond := range r.getMarkTrueDependents() {
=======
	for _, cond := range r.dependents {
>>>>>>> conditionals
		c := r.GetCondition(cond)
		// Failed or Unknown conditions trump true conditions
		if c == nil || c.Status != corev1.ConditionTrue {
			return
		}
	}

	// set the lead condition
	r.SetCondition(Condition{
		Type:   r.lead,
		Status: corev1.ConditionTrue,
	})
}

// MarkUnknown sets the status of t to Unknown and also sets the lead condition
// to Unknown if no other dependent condition is in an error state.
func (r conditionsImpl) MarkUnknown(t ConditionType, reason, message string) {
	// set the specified condition
	r.SetCondition(Condition{
		Type:   t,
		Status: corev1.ConditionUnknown,
	})

	// check the dependents.
<<<<<<< HEAD
	for _, cond := range r.getMarkUnknownDependents() {
=======
	for _, cond := range r.dependents {
>>>>>>> conditionals
		c := r.GetCondition(cond)
		// Failed conditions trump Unknown conditions
		if c == nil || c.Status == corev1.ConditionFalse {
			return
		}
	}

	// set the lead condition
	r.SetCondition(Condition{
		Type:    r.lead,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
}

// MarkFalse sets the status of t and the lead condition to False.
func (r conditionsImpl) MarkFalse(t ConditionType, reason, message string) {
	for _, t := range []ConditionType{
		t,
		r.lead,
	} {
		r.SetCondition(Condition{
			Type:    t,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	}
}

// InitializeConditions updates all Conditions in the ConditionSet to Unknown
// if not set.
func (r conditionsImpl) InitializeConditions() {
<<<<<<< HEAD
	for _, t := range append(r.getInitDependents(), r.lead) {
		r.InitializeCondition(t)
	}
}

// InitializeCondition updates a Condition to Unknown if not set.
func (r conditionsImpl) InitializeCondition(t ConditionType) {
	if c := r.GetCondition(t); c == nil {
		r.SetCondition(Condition{
			Type:   t,
			Status: corev1.ConditionUnknown,
		})
=======
	for _, t := range append(r.dependents, r.lead) {
		if c := r.GetCondition(t); c == nil {
			r.SetCondition(Condition{
				Type:   t,
				Status: corev1.ConditionUnknown,
			})
		}
>>>>>>> conditionals
	}
}
