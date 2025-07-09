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

package handler

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/metrics"
)

// NewMetricHandler creates a handler that collects and reports request metrics.
func NewMetricHandler(podName string, next http.Handler) *MetricHandler {
	return &MetricHandler{
		nextHandler: next,
		podName:     podName,
	}
}

// MetricHandler is a handler that records request metrics.
type MetricHandler struct {
	podName     string
	nextHandler http.Handler
}

func (h *MetricHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rev := RevisionFrom(r.Context())

	serviceName := rev.Labels[serving.ServiceLabelKey]
	configurationName := rev.Labels[serving.ConfigurationLabelKey]

	// otelhttp middleware creates the labeler
	labeler, _ := otelhttp.LabelerFromContext(r.Context())
	labeler.Add(
		metrics.ServiceNameKey.With(serviceName),
		metrics.ConfigurationNameKey.With(configurationName),
		metrics.RevisionNameKey.With(rev.Name),
		metrics.K8sNamespaceKey.With(rev.Namespace),
		metrics.ActivatorKeyValue,
	)

	h.nextHandler.ServeHTTP(w, r)
}
