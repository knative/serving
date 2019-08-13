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

	"go.uber.org/zap"

	"knative.dev/pkg/logging/logkey"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	servinglisters "knative.dev/serving/pkg/client/serving/listers/serving/v1alpha1"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/network"
)

// NewMetricHandler creates a handler collects and reports request metrics
func NewMetricHandler(rl servinglisters.RevisionLister, r activator.StatsReporter, l *zap.SugaredLogger, next http.Handler) *MetricHandler {
	handler := &MetricHandler{
		nextHandler:    next,
		revisionLister: rl,
		reporter:       r,
		logger:         l,
	}

	return handler
}

// MetricHandler sends metrics via reporter
type MetricHandler struct {
	revisionLister servinglisters.RevisionLister
	reporter       activator.StatsReporter
	logger         *zap.SugaredLogger
	nextHandler    http.Handler
}

func (h *MetricHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	// Filter out probe and healthy requests
	if network.IsKubeletProbe(r) || r.Header.Get(network.ProbeHeaderName) != "" {
		h.nextHandler.ServeHTTP(w, r)
		return
	}

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

	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	defer func() {
		err := recover()
		latency := time.Since(start)
		if err != nil {
			h.reporter.ReportResponseTime(namespace, serviceName, configurationName, name, http.StatusInternalServerError, latency)
			panic(err)
		}
		h.reporter.ReportResponseTime(namespace, serviceName, configurationName, name, rr.ResponseCode, latency)
	}()

	h.nextHandler.ServeHTTP(rr, r)
}
