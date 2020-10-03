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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
)

const (
	// MetricConditionReady is set when the Metric's latest
	// underlying revision has reported readiness.
	MetricConditionReady = apis.ConditionReady
)

var condSet = apis.NewLivingConditionSet(
	MetricConditionReady,
)

// GetConditionSet retrieves the condition set for this resource.
// Implements the KRShaped interface.
func (*Metric) GetConditionSet() apis.ConditionSet {
	return condSet
}

// GetGroupVersionKind implements OwnerRefable.
func (m *Metric) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Metric")
}

// GetCondition gets the condition `t`.
func (ms *MetricStatus) GetCondition(t apis.ConditionType) *apis.Condition {
	return condSet.Manage(ms).GetCondition(t)
}

// InitializeConditions initializes the conditions of the Metric.
func (ms *MetricStatus) InitializeConditions() {
	condSet.Manage(ms).InitializeConditions()
}

// MarkMetricReady marks the metric status as ready
func (ms *MetricStatus) MarkMetricReady() {
	condSet.Manage(ms).MarkTrue(MetricConditionReady)
}

// MarkMetricNotReady marks the metric status as ready == Unknown
func (ms *MetricStatus) MarkMetricNotReady(reason, message string) {
	condSet.Manage(ms).MarkUnknown(MetricConditionReady, reason, message)
}

// MarkMetricFailed marks the metric status as failed
func (ms *MetricStatus) MarkMetricFailed(reason, message string) {
	condSet.Manage(ms).MarkFalse(MetricConditionReady, reason, message)
}

// IsReady returns true if the Status condition MetricConditionReady
// is true and the latest spec has been observed.
func (m *Metric) IsReady() bool {
	ms := m.Status
	return ms.ObservedGeneration == m.Generation &&
		ms.GetCondition(MetricConditionReady).IsTrue()
}
