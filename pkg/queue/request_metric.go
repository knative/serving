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
	"net/http"
	"time"

	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"k8s.io/utils/clock"

	netheader "knative.dev/networking/pkg/http/header"
	pkghttp "knative.dev/serving/pkg/http"
)

var (
	scopeName     = "knative.dev/serving/pkg/queue"
	latencyBounds = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
)

type appRequestMetricsHandler struct {
	next    http.Handler
	breaker *Breaker
	clock   clock.Clock

	duration metric.Float64Histogram
	queueLen metric.Int64Gauge
}

// NewAppRequestMetricsHandler creates an http.Handler that emits request metrics.
func NewAppRequestMetricsHandler(
	mp metric.MeterProvider,
	next http.Handler,
	b *Breaker,
) (http.Handler, error) {
	var (
		meter   = mp.Meter(scopeName)
		err     error
		handler = &appRequestMetricsHandler{
			next:    next,
			breaker: b,
			clock:   clock.RealClock{},
		}
	)

	handler.duration, err = meter.Float64Histogram(
		"kn.queueproxy.app.duration",
		metric.WithDescription("The duration of task execution"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(latencyBounds...),
	)
	if err != nil {
		panic(err)
	}

	handler.queueLen, err = meter.Int64Gauge(
		"kn.queueproxy.depth",
		metric.WithDescription("Number of current items in the queue"),
		metric.WithUnit("{item}"),
	)
	if err != nil {
		panic(err)
	}

	return handler, nil
}

func (h *appRequestMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rr := pkghttp.NewResponseRecorder(w, http.StatusOK)
	startTime := h.clock.Now()

	if h.breaker != nil {
		h.queueLen.Record(r.Context(), int64(h.breaker.InFlight()))
	}
	defer func() {
		// Filter probe requests for revision metrics.
		if netheader.IsProbe(r) {
			return
		}

		// If ServeHTTP panics, recover, record the failure and panic again.
		err := recover()
		latency := h.clock.Since(startTime)

		status := rr.ResponseCode

		if err != nil {
			status = http.StatusInternalServerError
		}

		elapsedTime := float64(latency / time.Second)
		h.duration.Record(r.Context(), elapsedTime,
			metric.WithAttributes(semconv.HTTPResponseStatusCode(status)),
		)

		if err != nil {
			panic(err)
		}
	}()
	h.next.ServeHTTP(rr, r)
}
