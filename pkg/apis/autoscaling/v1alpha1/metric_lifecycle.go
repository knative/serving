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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
)

var metricCondSet = apis.NewLivingConditionSet(MetricConditionActive)

// GetGroupVersionKind implements OwnerRefable.
func (m *Metric) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Metric")
}

// IsReady looks at the conditions and if the Status has a condition
// ConditionReady returns true if ConditionStatus is True
func (ms *MetricStatus) IsReady() bool {
	return metricCondSet.Manage(ms.duck()).IsHappy()
}

// InitializeConditions initializes the conditions of the Metric.
func (ms *MetricStatus) InitializeConditions() {
	metricCondSet.Manage(ms.duck()).InitializeConditions()
}

// MarkActive marks the Metric active.
func (ms *MetricStatus) MarkActive() {
	metricCondSet.Manage(ms.duck()).MarkTrue(MetricConditionActive)
}

// MarkInactive marks the Metric as inactive.
func (ms *MetricStatus) MarkInactive(reason, message string) {
	metricCondSet.Manage(ms.duck()).MarkFalse(MetricConditionActive, reason, message)
}

func (ms *MetricStatus) duck() *duckv1beta1.Status {
	return (*duckv1beta1.Status)(&ms.Status)
}
