/*
Copyright 2018 The Knative Authors.

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
	corev1 "k8s.io/api/core/v1"
)

type conditionAccessor interface {
	setCondition(typ string, status corev1.ConditionStatus, reason string, message string)
	getConditionStatus(typ string) *corev1.ConditionStatus
}

type conditionManager struct {
	ready         string
	subconditions []string
}

func (c *conditionManager) initializeConditions(sc conditionAccessor) {
	for _, cond := range c.subconditions {
		if rc := sc.getConditionStatus(cond); rc == nil {
			c.setUnknownCondition(sc, cond, "", "")
		}
	}
}

func (c *conditionManager) setUnknownCondition(sc conditionAccessor, t string, reason, message string) {
	// Marking a subcondition as Unknown immediately flags Ready as Unknown.
	for _, cond := range []string{t, c.ready} {
		sc.setCondition(cond, corev1.ConditionUnknown, reason, message)
	}
}

func (c *conditionManager) setFalseCondition(sc conditionAccessor, t string, reason, message string) {
	// Marking a subcondition as False immediately flags Ready as False.
	for _, cond := range []string{t, c.ready} {
		sc.setCondition(cond, corev1.ConditionFalse, reason, message)
	}
}

func (c *conditionManager) setTrueCondition(sc conditionAccessor, t string, reason, message string) {
	sc.setCondition(t, corev1.ConditionTrue, reason, message)
	// Now, if ALL subconditions are True, mark Ready as True.
	for _, cond := range c.subconditions {
		cs := sc.getConditionStatus(cond)
		if cs == nil || *cs != corev1.ConditionTrue {
			return
		}
	}
	// Elide Reason/Message from Ready: True.
	sc.setCondition(c.ready, corev1.ConditionTrue, "", "")
}
