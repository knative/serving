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

package queue

import (
	"errors"
	"net/http"
	"time"

	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue/stats"
)

type requestMetricHandler struct {
	handler       http.Handler
	statsReporter stats.StatsReporter
	breaker       *Breaker
}

// NewRequestMetricHandler creates an http.Handler that emits request metrics.
func NewRequestMetricHandler(h http.Handler, r stats.StatsReporter, b *Breaker) (http.Handler, error) {
	if r == nil {
		return nil, errors.New("StatsReporter must not be nil")
	}

	return &requestMetricHandler{
		handler:       h,
		statsReporter: r,
		breaker:       b,
	}, nil
}

func (h *requestMetricHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	startTime := time.Now()
	if h.breaker != nil {
		h.statsReporter.ReportQueueDepth(h.breaker.InFlight())
	}

	defer func() {
		// Filter probe requests for revision metrics.
		if network.IsProbe(r) {
			return
		}

		// If ServeHTTP panics, recover, record the failure and panic again.
		err := recover()
		latency := time.Since(startTime)
		if err != nil {
			h.sendRequestMetrics(http.StatusInternalServerError, latency)
			panic(err)
		}
		h.sendRequestMetrics(rr.ResponseCode, latency)
	}()

	h.handler.ServeHTTP(rr, r)
}

func (h *requestMetricHandler) sendRequestMetrics(respCode int, latency time.Duration) {
	h.statsReporter.ReportRequestCount(respCode)
	h.statsReporter.ReportResponseTime(respCode, latency)
}
