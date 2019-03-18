/*
Copyright 2019 The Knative Authors.

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
	"fmt"
	"strconv"
	"time"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/serving/pkg/apis/autoscaling"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var podCondSet = duckv1alpha1.NewLivingConditionSet(
	PodAutoscalerConditionActive,
)

func (pa *PodAutoscaler) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("PodAutoscaler")
}

func (pa *PodAutoscaler) Class() string {
	if c, ok := pa.Annotations[autoscaling.ClassAnnotationKey]; ok {
		return c
	}
	// Default to "kpa" class for backward compatibility.
	return autoscaling.KPA
}

func (pa *PodAutoscaler) annotationInt32(key string) int32 {
	if s, ok := pa.Annotations[key]; ok {
		// no error check: relying on validation
		i, _ := strconv.ParseInt(s, 10, 32)
		if i < 0 {
			return 0
		}
		return int32(i)
	}
	return 0
}

// ScaleBounds returns scale bounds annotations values as a tuple:
// `(min, max int32)`. The value of 0 for any of min or max means the bound is
// not set
func (pa *PodAutoscaler) ScaleBounds() (min, max int32) {
	min = pa.annotationInt32(autoscaling.MinScaleAnnotationKey)
	max = pa.annotationInt32(autoscaling.MaxScaleAnnotationKey)
	return
}

// Target returns the target annotation value or false if not present.
func (pa *PodAutoscaler) Target() (target int32, ok bool) {
	if s, ok := pa.Annotations[autoscaling.TargetAnnotationKey]; ok {
		if i, err := strconv.Atoi(s); err == nil {
			if i < 1 {
				return 0, false
			}
			return int32(i), true
		}
	}
	return 0, false
}

// IsReady looks at the conditions and if the Status has a condition
// PodAutoscalerConditionReady returns true if ConditionStatus is True
func (rs *PodAutoscalerStatus) IsReady() bool {
	return podCondSet.Manage(rs).IsHappy()
}

// IsActivating assumes the pod autoscaler is Activating if it is neither
// Active nor Inactive
func (rs *PodAutoscalerStatus) IsActivating() bool {
	cond := rs.GetCondition(PodAutoscalerConditionActive)

	return cond != nil && cond.Status == corev1.ConditionUnknown
}

func (rs *PodAutoscalerStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return podCondSet.Manage(rs).GetCondition(t)
}

func (rs *PodAutoscalerStatus) InitializeConditions() {
	podCondSet.Manage(rs).InitializeConditions()
}

func (rs *PodAutoscalerStatus) MarkActive() {
	podCondSet.Manage(rs).MarkTrue(PodAutoscalerConditionActive)
}

func (rs *PodAutoscalerStatus) MarkActivating(reason, message string) {
	podCondSet.Manage(rs).MarkUnknown(PodAutoscalerConditionActive, reason, message)
}

func (rs *PodAutoscalerStatus) MarkInactive(reason, message string) {
	podCondSet.Manage(rs).MarkFalse(PodAutoscalerConditionActive, reason, message)
}

// MarkResourceNotOwned changes the "Active" condition to false to reflect that the
// resource of the given kind and name has already been created, and we do not own it.
func (rs *PodAutoscalerStatus) MarkResourceNotOwned(kind, name string) {
	rs.MarkInactive("NotOwned",
		fmt.Sprintf("There is an existing %s %q that we do not own.", kind, name))
}

// MarkResourceFailedCreation changes the "Active" condition to false to reflect that a
// critical resource of the given kind and name was unable to be created.
func (rs *PodAutoscalerStatus) MarkResourceFailedCreation(kind, name string) {
	rs.MarkInactive("FailedCreate",
		fmt.Sprintf("Failed to create %s %q.", kind, name))
}

// CanScaleToZero checks whether the pod autoscaler has been in an inactive state
// for at least the specified grace period.
func (rs *PodAutoscalerStatus) CanScaleToZero(gracePeriod time.Duration) bool {
	if cond := rs.GetCondition(PodAutoscalerConditionActive); cond != nil {
		switch cond.Status {
		case corev1.ConditionFalse:
			// Check that this PodAutoscaler has been inactive for
			// at least the grace period.
			return time.Now().After(cond.LastTransitionTime.Inner.Add(gracePeriod))
		}
	}
	return false
}

// CanMarkInactive checks whether the pod autoscaler has been in an active state
// for at least the specified idle period.
func (rs *PodAutoscalerStatus) CanMarkInactive(idlePeriod time.Duration) bool {
	if cond := rs.GetCondition(PodAutoscalerConditionActive); cond != nil {
		switch cond.Status {
		case corev1.ConditionTrue:
			// Check that this PodAutoscaler has been active for
			// at least the grace period.
			return time.Now().After(cond.LastTransitionTime.Inner.Add(idlePeriod))
		}
	}
	return false
}
