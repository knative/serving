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
	"math"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	cmetrics "k8s.io/metrics/pkg/apis/custom_metrics"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

var (
	concurrencyMetricInfo = provider.CustomMetricInfo{
		GroupResource: v1alpha1.Resource("revisions"),
		Namespaced:    true,
		Metric:        autoscaling.Concurrency,
	}
	rpsMetricInfo = provider.CustomMetricInfo{
		GroupResource: v1alpha1.Resource("revisions"),
		Namespaced:    true,
		Metric:        autoscaling.RPS,
	}

	errMetricNotSupported = errors.New("metric not supported")
	errNotImplemented     = errors.New("not implemented")
)

// MetricProvider is a provider to back a custom-metrics API implementation.
type MetricProvider struct {
	metricClient MetricClient
}

var _ provider.CustomMetricsProvider = (*MetricProvider)(nil)

// NewMetricProvider creates a new MetricProvider.
func NewMetricProvider(metricClient MetricClient) *MetricProvider {
	return &MetricProvider{
		metricClient: metricClient,
	}
}

// GetMetricByName implements the interface.
func (p *MetricProvider) GetMetricByName(name types.NamespacedName, info provider.CustomMetricInfo,
	metricSelector labels.Selector) (*cmetrics.MetricValue, error) {
	now := time.Now()
	var data float64
	var err error
	if cmp.Equal(info, concurrencyMetricInfo) {
		data, _, err = p.metricClient.StableAndPanicConcurrency(name, now)
	} else if cmp.Equal(info, rpsMetricInfo) {
		data, _, err = p.metricClient.StableAndPanicRPS(name, now)
	} else {
		return nil, errMetricNotSupported
	}
	if err != nil {
		return nil, err
	}

	return &cmetrics.MetricValue{
		Metric: cmetrics.MetricIdentifier{
			Name: info.Metric,
		},
		Timestamp: metav1.Time{Time: now},
		Value:     *resource.NewQuantity(int64(math.Ceil(data)), resource.DecimalSI),
	}, nil
}

// GetMetricBySelector implements the interface.
func (p *MetricProvider) GetMetricBySelector(string, labels.Selector, provider.CustomMetricInfo, labels.Selector) (*cmetrics.MetricValueList, error) {
	return nil, errNotImplemented
}

// ListAllMetrics implements the interface.
func (p *MetricProvider) ListAllMetrics() []provider.CustomMetricInfo {
	return []provider.CustomMetricInfo{concurrencyMetricInfo, rpsMetricInfo}
}
