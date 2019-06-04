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

package autoscaler

import (
	"errors"

	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/metrics/pkg/apis/custom_metrics"
)

// MetricProvider is a provider to back a custom-metrics API implementation.
type MetricProvider struct{}

var _ provider.CustomMetricsProvider = (*MetricProvider)(nil)

// NewMetricProvider creates a new MetricProvider.
func NewMetricProvider() *MetricProvider {
	return &MetricProvider{}
}

// GetMetricByName implements the interface.
func (p *MetricProvider) GetMetricByName(name types.NamespacedName, info provider.CustomMetricInfo) (*custom_metrics.MetricValue, error) {
	return nil, errors.New("not implemented")
}

// GetMetricBySelector implements the interface.
func (p *MetricProvider) GetMetricBySelector(namespace string, selector labels.Selector, info provider.CustomMetricInfo) (*custom_metrics.MetricValueList, error) {
	return nil, errors.New("not implemented")
}

// ListAllMetrics implements the interface.
func (p *MetricProvider) ListAllMetrics() []provider.CustomMetricInfo {
	return []provider.CustomMetricInfo{}
}
