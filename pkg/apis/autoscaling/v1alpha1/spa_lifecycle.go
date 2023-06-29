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
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmap"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
)

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*StagePodAutoscaler) GetConditionSet() apis.ConditionSet {
	return podCondSet
}

// GetGroupVersionKind returns the GVK for the PodAutoscaler.
func (pa *StagePodAutoscaler) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("PodAutoscaler")
}

// Class returns the Autoscaler class from Annotation or `KPA` if none is set.
func (pa *StagePodAutoscaler) Class() string {
	if c, ok := pa.Annotations[autoscaling.ClassAnnotationKey]; ok {
		return c
	}
	// Default to "kpa" class for backward compatibility.
	return autoscaling.KPA
}

// Metric returns the contents of the metric annotation or a default.
func (pa *StagePodAutoscaler) Metric() string {
	if m, ok := pa.Annotations[autoscaling.MetricAnnotationKey]; ok {
		return m
	}
	// TODO: defaulting here is awkward and is already taken care of by defaulting logic.
	return defaultMetric(pa.Class())
}

func (pa *StagePodAutoscaler) annotationInt32(k kmap.KeyPriority) (int32, bool) {
	if _, s, ok := k.Get(pa.Annotations); ok {
		i, err := strconv.ParseInt(s, 10, 32)
		return int32(i), err == nil
	}
	return 0, false
}

func (pa *StagePodAutoscaler) annotationFloat64(k kmap.KeyPriority) (float64, bool) {
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
func (pa *StagePodAutoscaler) ScaleBounds(asConfig *autoscalerconfig.Config) (*int32, *int32) {

	return pa.Spec.MinScale, pa.Spec.MaxScale
}

// ActivationScale returns the min-non-zero-replicas annotation value or falise
// if not present or invalid.
func (pa *StagePodAutoscaler) ActivationScale() (int32, bool) {
	return pa.annotationInt32(autoscaling.ActivationScale)
}

// Target returns the target annotation value or false if not present, or invalid.
func (pa *StagePodAutoscaler) Target() (float64, bool) {
	return pa.annotationFloat64(autoscaling.TargetAnnotation)
}

// TargetUtilization returns the target utilization percentage as a fraction, if
// the corresponding annotation is set.
func (pa *StagePodAutoscaler) TargetUtilization() (float64, bool) {
	if tu, ok := pa.annotationFloat64(autoscaling.TargetUtilizationPercentageAnnotation); ok {
		return tu / 100, true
	}
	return 0, false
}

// TargetBC returns the target burst capacity, if the corresponding annotation is set.
func (pa *StagePodAutoscaler) TargetBC() (float64, bool) {
	// The value is validated in the webhook.
	return pa.annotationFloat64(autoscaling.TargetBurstCapacityAnnotation)
}

func (pa *StagePodAutoscaler) annotationDuration(k kmap.KeyPriority) (time.Duration, bool) {
	if _, s, ok := k.Get(pa.Annotations); ok {
		d, err := time.ParseDuration(s)
		return d, err == nil
	}
	return 0, false
}

// ScaleToZeroPodRetention returns the ScaleToZeroPodRetention annotation value,
// or false if not present.
func (pa *StagePodAutoscaler) ScaleToZeroPodRetention() (time.Duration, bool) {
	// The value is validated in the webhook.
	return pa.annotationDuration(autoscaling.ScaleToZeroPodRetentionPeriodAnnotation)
}

// Window returns the window annotation value, or false if not present.
func (pa *StagePodAutoscaler) Window() (time.Duration, bool) {
	// The value is validated in the webhook.
	return pa.annotationDuration(autoscaling.WindowAnnotation)
}

// ScaleDownDelay returns the scale down delay annotation, or false if not present.
func (pa *StagePodAutoscaler) ScaleDownDelay() (time.Duration, bool) {
	// The value is validated in the webhook.
	return pa.annotationDuration(autoscaling.ScaleDownDelayAnnotation)
}

// PanicWindowPercentage returns the panic window annotation value, or false if not present.
func (pa *StagePodAutoscaler) PanicWindowPercentage() (percentage float64, ok bool) {
	// The value is validated in the webhook.
	return pa.annotationFloat64(autoscaling.PanicWindowPercentageAnnotation)
}

// PanicThresholdPercentage returns the panic threshold annotation value, or false if not present.
func (pa *StagePodAutoscaler) PanicThresholdPercentage() (percentage float64, ok bool) {
	// The value is validated in the webhook.
	return pa.annotationFloat64(autoscaling.PanicThresholdPercentageAnnotation)
}

// ProgressDeadline returns the progress deadline annotation value, or false if not present.
func (pa *StagePodAutoscaler) ProgressDeadline() (time.Duration, bool) {
	// the value is validated in the webhook
	return pa.annotationDuration(serving.ProgressDeadlineAnnotation)
}

// InitialScale returns the initial scale on the revision if present, or false if not present.
func (pa *StagePodAutoscaler) InitialScale() (int32, bool) {
	// The value is validated in the webhook.
	return pa.annotationInt32(autoscaling.InitialScaleAnnotation)
}
