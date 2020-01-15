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
	"context"
	"net/http"
	"time"

	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	pkghttp "knative.dev/serving/pkg/http"
)

// NewMetricHandler creates a handler collects and reports request metrics
func NewMetricHandler(ctx context.Context, r activator.StatsReporter, next http.Handler) *MetricHandler {
	handler := &MetricHandler{
		nextHandler: next,
		reporter:    r,
	}

	return handler
}

// MetricHandler sends metrics via reporter
type MetricHandler struct {
	reporter    activator.StatsReporter
	nextHandler http.Handler
}

func (h *MetricHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	revID := revIDFrom(r.Context())
	revision := revisionFrom(r.Context())
	configurationName := revision.Labels[serving.ConfigurationLabelKey]
	serviceName := revision.Labels[serving.ServiceLabelKey]
	start := time.Now()

	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	defer func() {
		err := recover()
		latency := time.Since(start)
		if err != nil {
			h.reporter.ReportResponseTime(revID.Namespace, serviceName, configurationName, revID.Name, http.StatusInternalServerError, latency)
			panic(err)
		}
		h.reporter.ReportResponseTime(revID.Namespace, serviceName, configurationName, revID.Name, rr.ResponseCode, latency)
	}()

	h.nextHandler.ServeHTTP(rr, r)
}
