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
	"strconv"
	"time"

	"go.uber.org/zap"

	"knative.dev/pkg/logging/logkey"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	pkghttp "knative.dev/serving/pkg/http"
)

// NewRequestMetricHandler creates a handler that sends metrics to autoscaler
func NewRequestMetricHandler(rl servinglisters.RevisionLister, r activator.StatsReporter, l *zap.SugaredLogger, next http.Handler) *RequestMetricHandler {
	handler := &RequestMetricHandler{
		nextHandler:    next,
		revisionLister: rl,
		reporter:       r,
		logger:         l,
	}

	return handler
}

// RequestMetricHandler sends metrics to reporter.
type RequestMetricHandler struct {
	revisionLister servinglisters.RevisionLister
	reporter       activator.StatsReporter
	logger         *zap.SugaredLogger
	nextHandler    http.Handler
}

func (h *RequestMetricHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := r.Header.Get(activator.RevisionHeaderNamespace)
	name := r.Header.Get(activator.RevisionHeaderName)

	revID := activator.RevisionID{Namespace: namespace, Name: name}
	logger := h.logger.With(zap.String(logkey.Key, revID.String()))

	revision, err := h.revisionLister.Revisions(namespace).Get(name)
	if err != nil {
		logger.Errorw("Error while getting revision", zap.Error(err))
		sendError(err, w)
		return
	}
	configurationName := revision.Labels[serving.ConfigurationLabelKey]
	serviceName := revision.Labels[serving.ServiceLabelKey]
	start := time.Now()
	//attempts would default to 1 if no attempt header is found.
	var attempts = int(1)

	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	defer func() {
		if v, err := strconv.Atoi(rr.Header().Get(activator.ProxyAttempts)); err == nil {
			attempts = v
			rr.Header().Del(activator.ProxyAttempts)
		}
		err := recover()
		latency := time.Since(start)
		if err != nil {
			h.reportRequestMetrics(namespace, serviceName, configurationName, name, http.StatusInternalServerError, latency, attempts)
			panic(err)
		}
		h.reportRequestMetrics(namespace, serviceName, configurationName, name, rr.ResponseCode, latency, attempts)
	}()

	h.nextHandler.ServeHTTP(rr, r)
}

func (h *RequestMetricHandler) reportRequestMetrics(namespace, serviceName, configurationName,
	name string, responseCode int, d time.Duration, retries int) {
	h.reporter.ReportRequestCount(namespace, serviceName, configurationName, name, responseCode, retries, 1.0)
	h.reporter.ReportResponseTime(namespace, serviceName, configurationName, name, responseCode, d)
}
