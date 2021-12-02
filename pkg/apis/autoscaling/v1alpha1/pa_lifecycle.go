/*
Copyright 2019 The Knative Authors

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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmap"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
)

var podCondSet = apis.NewLivingConditionSet(
	PodAutoscalerConditionActive,
	PodAutoscalerConditionScaleTargetInitialized,
	PodAutoscalerConditionSKSReady,
)

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*PodAutoscaler) GetConditionSet() apis.ConditionSet {
	return podCondSet
}

// GetGroupVersionKind returns the GVK for the PodAutoscaler.
func (pa *PodAutoscaler) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("PodAutoscaler")
}

// Class returns the Autoscaler class from Annotation or `KPA` if none is set.
func (pa *PodAutoscaler) Class() string {
	if c, ok := pa.Annotations[autoscaling.ClassAnnotationKey]; ok {
		return c
	}
	// Default to "kpa" class for backward compatibility.
	return autoscaling.KPA
}

// Metric returns the contents of the metric annotation or a default.
func (pa *PodAutoscaler) Metric() string {
	if m, ok := pa.Annotations[autoscaling.MetricAnnotationKey]; ok {
		return m
	}
	// TODO: defaulting here is awkward and is already taken care of by defaulting logic.
	return defaultMetric(pa.Class())
}

func (pa *PodAutoscaler) annotationInt32(k kmap.KeyPriority) (int32, bool) {
	if _, s, ok := k.Get(pa.Annotations); ok {
		i, err := strconv.ParseInt(s, 10, 32)
		return int32(i), err == nil
	}
	return 0, false
}

func (pa *PodAutoscaler) annotationFloat64(k kmap.KeyPriority) (float64, bool) {
	if _, s, ok := k.Get(pa.Annotations); ok {
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	}
	return 0.0, false
}

// ScaleBounds returns scale bounds annotations values as a tuple:
// `(min, max int32)`. The value of 0 for any of min or max means the bound is
// not set.
// Note: min will be ignored if the PA is not reachable
func (pa *PodAutoscaler) ScaleBounds(asConfig *autoscalerconfig.Config) (int32, int32) {
	var min int32
	if pa.Spec.Reachability != ReachabilityUnreachable {
		min = asConfig.MinScale
		if paMin, ok := pa.annotationInt32(autoscaling.MinScaleAnnotation); ok {
			min = paMin
		}
	}

	max := asConfig.MaxScale
	if paMax, ok := pa.annotationInt32(autoscaling.MaxScaleAnnotation); ok {
		max = paMax
	}

	return min, max
}

// Target returns the target annotation value or false if not present, or invalid.
func (pa *PodAutoscaler) Target() (float64, bool) {
	return pa.annotationFloat64(autoscaling.TargetAnnotation)
}

// TargetUtilization returns the target utilization percentage as a fraction, if
// the corresponding annotation is set.
func (pa *PodAutoscaler) TargetUtilization() (float64, bool) {
	if tu, ok := pa.annotationFloat64(autoscaling.TargetUtilizationPercentageAnnotation); ok {
		return tu / 100, true
	}
	return 0, false
}

// TargetBC returns the target burst capacity, if the corresponding annotation is set.
func (pa *PodAutoscaler) TargetBC() (float64, bool) {
	// The value is validated in the webhook.
	return pa.annotationFloat64(autoscaling.TargetBurstCapacityAnnotation)
}

func (pa *PodAutoscaler) annotationDuration(k kmap.KeyPriority) (time.Duration, bool) {
	if _, s, ok := k.Get(pa.Annotations); ok {
		d, err := time.ParseDuration(s)
		return d, err == nil
	}
	return 0, false
}

// ScaleToZeroPodRetention returns the ScaleToZeroPodRetention annotation value,
// or false if not present.
func (pa *PodAutoscaler) ScaleToZeroPodRetention() (time.Duration, bool) {
	// The value is validated in the webhook.
	return pa.annotationDuration(autoscaling.ScaleToZeroPodRetentionPeriodAnnotation)
}

// Window returns the window annotation value, or false if not present.
func (pa *PodAutoscaler) Window() (time.Duration, bool) {
	// The value is validated in the webhook.
	return pa.annotationDuration(autoscaling.WindowAnnotation)
}

// ScaleDownDelay returns the scale down delay annotation, or false if not present.
func (pa *PodAutoscaler) ScaleDownDelay() (time.Duration, bool) {
	// The value is validated in the webhook.
	return pa.annotationDuration(autoscaling.ScaleDownDelayAnnotation)
}

// PanicWindowPercentage returns the panic window annotation value, or false if not present.
func (pa *PodAutoscaler) PanicWindowPercentage() (percentage float64, ok bool) {
	// The value is validated in the webhook.
	return pa.annotationFloat64(autoscaling.PanicWindowPercentageAnnotation)
}

// PanicThresholdPercentage returns the panic threshold annotation value, or false if not present.
func (pa *PodAutoscaler) PanicThresholdPercentage() (percentage float64, ok bool) {
	// The value is validated in the webhook.
	return pa.annotationFloat64(autoscaling.PanicThresholdPercentageAnnotation)
}

// InitialScale returns the initial scale on the revision if present, or false if not present.
func (pa *PodAutoscaler) InitialScale() (int32, bool) {
	// The value is validated in the webhook.
	return pa.annotationInt32(autoscaling.InitialScaleAnnotation)
}

// IsReady returns true if the Status condition PodAutoscalerConditionReady
// is true and the latest spec has been observed.
func (pa *PodAutoscaler) IsReady() bool {
	pas := pa.Status
	return pa.Generation == pas.ObservedGeneration &&
		pas.GetCondition(PodAutoscalerConditionReady).IsTrue()
}

// IsActive returns true if the pod autoscaler has finished scaling.
func (pas *PodAutoscalerStatus) IsActive() bool {
	return pas.GetCondition(PodAutoscalerConditionActive).IsTrue()
}

// IsActivating returns true if the pod autoscaler is Activating if it is neither
// Active nor Inactive.
func (pas *PodAutoscalerStatus) IsActivating() bool {
	return pas.GetCondition(PodAutoscalerConditionActive).IsUnknown()
}

// IsInactive returns true if the pod autoscaler is Inactive.
func (pas *PodAutoscalerStatus) IsInactive() bool {
	return pas.GetCondition(PodAutoscalerConditionActive).IsFalse()
}

// IsScaleTargetInitialized returns true if the PodAutoscaler's scale target has been
// initialized successfully.
func (pas *PodAutoscalerStatus) IsScaleTargetInitialized() bool {
	return pas.GetCondition(PodAutoscalerConditionScaleTargetInitialized).IsTrue()
}

// MarkScaleTargetInitialized marks the PA's PodAutoscalerConditionScaleTargetInitialized
// condition true.
func (pas *PodAutoscalerStatus) MarkScaleTargetInitialized() {
	podCondSet.Manage(pas).MarkTrue(PodAutoscalerConditionScaleTargetInitialized)
}

// MarkSKSReady marks the PA condition denoting that SKS is ready.
func (pas *PodAutoscalerStatus) MarkSKSReady() {
	podCondSet.Manage(pas).MarkTrue(PodAutoscalerConditionSKSReady)
}

// MarkSKSNotReady marks the PA condition that denotes SKS is not yet ready.
func (pas *PodAutoscalerStatus) MarkSKSNotReady(mes string) {
	podCondSet.Manage(pas).MarkUnknown(PodAutoscalerConditionSKSReady, "NotReady", mes)
}

// GetCondition gets the condition `t`.
func (pas *PodAutoscalerStatus) GetCondition(t apis.ConditionType) *apis.Condition {
	return podCondSet.Manage(pas).GetCondition(t)
}

// InitializeConditions initializes the conditions of the PA.
func (pas *PodAutoscalerStatus) InitializeConditions() {
	podCondSet.Manage(pas).InitializeConditions()
}

// MarkActive marks the PA as active.
func (pas *PodAutoscalerStatus) MarkActive() {
	podCondSet.Manage(pas).MarkTrue(PodAutoscalerConditionActive)
}

// MarkActivating marks the PA as activating.
func (pas *PodAutoscalerStatus) MarkActivating(reason, message string) {
	podCondSet.Manage(pas).MarkUnknown(PodAutoscalerConditionActive, reason, message)
}

// MarkInactive marks the PA as inactive.
func (pas *PodAutoscalerStatus) MarkInactive(reason, message string) {
	podCondSet.Manage(pas).MarkFalse(PodAutoscalerConditionActive, reason, message)
}

// MarkResourceNotOwned changes the "Active" condition to false to reflect that the
// resource of the given kind and name has already been created, and we do not own it.
func (pas *PodAutoscalerStatus) MarkResourceNotOwned(kind, name string) {
	pas.MarkInactive("NotOwned",
		fmt.Sprintf("There is an existing %s %q that we do not own.", kind, name))
}

// MarkResourceFailedCreation changes the "Active" condition to false to reflect that a
// critical resource of the given kind and name was unable to be created.
func (pas *PodAutoscalerStatus) MarkResourceFailedCreation(kind, name string) {
	pas.MarkInactive("FailedCreate",
		fmt.Sprintf("Failed to create %s %q.", kind, name))
}

// InactiveFor returns the time PA spent being inactive.
func (pas *PodAutoscalerStatus) InactiveFor(now time.Time) time.Duration {
	return pas.inStatusFor(corev1.ConditionFalse, now)
}

// ActiveFor returns the time PA spent being active.
func (pas *PodAutoscalerStatus) ActiveFor(now time.Time) time.Duration {
	return pas.inStatusFor(corev1.ConditionTrue, now)
}

// CanFailActivation checks whether the pod autoscaler has been activating
// for at least the specified idle period.
func (pas *PodAutoscalerStatus) CanFailActivation(now time.Time, idlePeriod time.Duration) bool {
	return pas.inStatusFor(corev1.ConditionUnknown, now) > idlePeriod
}

// inStatusFor returns the duration that the PodAutoscalerStatus's Active
// condition has stayed in the specified status.
// inStatusFor will return -1 if condition is not initialized or current
// status is different.
func (pas *PodAutoscalerStatus) inStatusFor(status corev1.ConditionStatus, now time.Time) time.Duration {
	cond := pas.GetCondition(PodAutoscalerConditionActive)
	if cond == nil || cond.Status != status {
		return -1
	}
	return now.Sub(cond.LastTransitionTime.Inner.Time)
}

// GetDesiredScale returns the desired scale if ever set, or -1.
func (pas *PodAutoscalerStatus) GetDesiredScale() int32 {
	if pas.DesiredScale != nil {
		return *pas.DesiredScale
	}
	return -1
}

// GetActualScale returns the actual scale if ever set, or -1.
func (pas *PodAutoscalerStatus) GetActualScale() int32 {
	if pas.ActualScale != nil {
		return *pas.ActualScale
	}
	return -1
}
