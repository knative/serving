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
	"time"

	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	pkghttp "knative.dev/serving/pkg/http"
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
	reporterCtx, _ := metrics.PodRevisionContext(h.podName, activator.Name,
		rev.Namespace, rev.Labels[serving.ServiceLabelKey], rev.Labels[serving.ConfigurationLabelKey], rev.Name, rev.GetAnnotations(), rev.GetLabels())

	start := time.Now()

	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	defer func() {
		err := recover()
		latency := time.Since(start)
		if err != nil {
			reporterCtx := metrics.AugmentWithResponse(reporterCtx, http.StatusInternalServerError)
			pkgmetrics.RecordBatch(reporterCtx, responseTimeInMsecM.M(float64(latency.Milliseconds())), requestCountM.M(1))
			panic(err)
		}
		reporterCtx := metrics.AugmentWithResponse(reporterCtx, rr.ResponseCode)
		pkgmetrics.RecordBatch(reporterCtx, responseTimeInMsecM.M(float64(latency.Milliseconds())), requestCountM.M(1))
	}()

	h.nextHandler.ServeHTTP(rr, r)
}
