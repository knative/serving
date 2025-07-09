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
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	clocktest "k8s.io/utils/clock/testing"

	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/observability/metrics/metricstest"
)

const targetURI = "http://example.com"

func TestAppRequestMetricsHandlerPanickingHandler(t *testing.T) {
	// Use the fake clock to set the duration
	fakeClock := clocktest.NewFakeClock(time.Now())

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// advance time by one second
		fakeClock.SetTime(fakeClock.Now().Add(time.Second))
		panic("no!")
	})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})

	// Increment the breaker to report 1 active request
	breaker.tryAcquirePending()
	handler, err := NewAppRequestMetricsHandler(mp, baseHandler, breaker)

	handler.(*appRequestMetricsHandler).clock = fakeClock

	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	defer func() {
		if err := recover(); err == nil {
			t.Error("Want ServeHTTP to panic, got nothing.")
		}

		assertMetrics(t, reader, http.StatusInternalServerError)
	}()

	handler.ServeHTTP(resp, req)
}

func TestAppRequestMetricsHandler(t *testing.T) {
	// Use the fake clock to set the duration
	fakeClock := clocktest.NewFakeClock(time.Now())

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fakeClock.SetTime(fakeClock.Now().Add(time.Second))
	})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	breaker.tryAcquirePending()
	handler, err := NewAppRequestMetricsHandler(mp, baseHandler, breaker)
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	handler.(*appRequestMetricsHandler).clock = fakeClock

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	handler.ServeHTTP(resp, req)

	assertMetrics(t, reader, http.StatusOK)

	// A probe request should not be recorded.
	req.Header.Set(netheader.ProbeKey, "activator")
	handler.ServeHTTP(resp, req)

	// should be no changes so we run the same assertion
	assertMetrics(t, reader, http.StatusOK)
}

func assertMetrics(t *testing.T, reader *metric.ManualReader, status int) {
	t.Helper()

	bucketCounts := [15]uint64{}
	bucketCounts[9] = 1 // one second bucket

	metricstest.AssertMetrics(
		t, reader,
		metricstest.MetricsEqual(
			scopeName,
			metricdata.Metrics{
				Name:        "kn.queueproxy.depth",
				Unit:        "{item}",
				Description: "Number of current items in the queue",
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Value: 1},
					},
				},
			},
			metricdata.Metrics{
				Name:        "kn.queueproxy.app.duration",
				Unit:        "s",
				Description: "The duration of task execution",
				Data: metricdata.Histogram[float64]{
					Temporality: metricdata.CumulativeTemporality,
					DataPoints: []metricdata.HistogramDataPoint[float64]{{
						Bounds:       latencyBounds,
						Count:        1,
						Sum:          1,
						BucketCounts: bucketCounts[:],
						Min:          metricdata.NewExtrema[float64](1),
						Max:          metricdata.NewExtrema[float64](1),
						Attributes: attribute.NewSet(
							semconv.HTTPResponseStatusCode(status),
						),
					}},
				},
			},
		),
	)
}

func BenchmarkAppRequestMetricsHandler(b *testing.B) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(mp, baseHandler, breaker)
	if err != nil {
		b.Fatal("Failed to create handler:", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	b.Run("sequential", func(b *testing.B) {
		resp := httptest.NewRecorder()
		for b.Loop() {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			resp := httptest.NewRecorder()
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}
