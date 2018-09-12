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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/pkg/v1alpha1"
)

type Conditional interface {
	GetConditions() []v1alpha1.Condition
	SetConditions([]v1alpha1.Condition)
}

func NewConditionSet(l v1alpha1.ConditionType, d ...v1alpha1.ConditionType) *ConditionSet {
	c := &ConditionSet{
		lead:       l,
		dependents: d,
	}
	return c
}

type ConditionSet struct {
	lead       v1alpha1.ConditionType
	dependents []v1alpha1.ConditionType
}

type ConditionSetImpl struct {
	using      Conditional
	lead       v1alpha1.ConditionType
	dependents []v1alpha1.ConditionType
}

func (r *ConditionSet) Using(conditional Conditional) *ConditionSetImpl {
	return &ConditionSetImpl{
		using:      conditional,
		lead:       r.lead,
		dependents: r.dependents,
	}
}

// IsReady looks at the lead condition and returns true if that condition is
// set to true.
func (r *ConditionSetImpl) IsReady() bool {
	if c := r.GetCondition(r.lead); c == nil || !c.IsTrue() {
		return false
	}
	return true
}

// GetCondition finds and returns the Condition that matches the ConditionType previously set on Conditions.
func (r *ConditionSetImpl) GetCondition(t v1alpha1.ConditionType) *v1alpha1.Condition {
	if r.using == nil {
		return nil
	}
	for _, c := range r.using.GetConditions() {
		if c.Type == t {
			return &c
		}
	}
	return nil
}

// SetCondition sets or updates the Condition on Conditions for Condition.Type.
// returns the mutated list of Conditions.
func (r *ConditionSetImpl) SetCondition(new *v1alpha1.Condition) {
	if r.using == nil {
		return
	}
	if new == nil {
		return
	}
	t := new.Type
	var conditions []v1alpha1.Condition
	for _, c := range r.using.GetConditions() {
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
	r.using.SetConditions(conditions)
}

// MarkTrue sets the status of t to true.
// returns the mutated list of Conditions.
func (r *ConditionSetImpl) MarkTrue(t v1alpha1.ConditionType) {
	// set the specified condition
	r.SetCondition(&v1alpha1.Condition{
		Type:   t,
		Status: v1alpha1.ConditionTrue,
	})

	// check the dependents.
	for _, cond := range r.dependents {
		c := r.GetCondition(cond)
		// Failed or Unknown conditions trump true conditions
		if c == nil || c.Status != v1alpha1.ConditionTrue {
			return
		}
	}

	// set the lead condition
	r.SetCondition(&v1alpha1.Condition{
		Type:   r.lead,
		Status: v1alpha1.ConditionTrue,
	})
}

// MarkUnknown sets the status of t to true.
// returns the mutated list of Conditions.
func (r *ConditionSetImpl) MarkUnknown(t v1alpha1.ConditionType, reason, message string) {
	// set the specified condition
	r.SetCondition(&v1alpha1.Condition{
		Type:   t,
		Status: v1alpha1.ConditionUnknown,
	})

	// check the dependents.
	for _, cond := range r.dependents {
		c := r.GetCondition(cond)
		// Failed conditions trump unknown conditions
		if c == nil || c.Status == v1alpha1.ConditionFalse {
			return
		}
	}

	// set the lead condition
	r.SetCondition(&v1alpha1.Condition{
		Type:    r.lead,
		Status:  v1alpha1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
}

// MarkFalse sets the status of t to true.
// returns the mutated list of Conditions.
func (r *ConditionSetImpl) MarkFalse(t v1alpha1.ConditionType, reason, message string) {
	for _, t := range []v1alpha1.ConditionType{
		t,
		r.lead,
	} {
		r.SetCondition(&v1alpha1.Condition{
			Type:    t,
			Status:  v1alpha1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	}
}

// InitializeConditions updates the Ready Condition to unknown if not set.
// returns the mutated list of Conditions.
func (r *ConditionSetImpl) InitializeConditions() {
	for _, t := range append(r.dependents, r.lead) {
		r.SetCondition(&v1alpha1.Condition{
			Type:   t,
			Status: v1alpha1.ConditionUnknown,
		})
	}
}
