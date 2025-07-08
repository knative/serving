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

	"go.opentelemetry.io/otel/sdk/metric"
	netheader "knative.dev/networking/pkg/http/header"
)

const targetURI = "http://example.com"

func TestAppRequestMetricsHandlerPanickingHandler(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no!")
	})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(mp, baseHandler, breaker)
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	defer func() {
		if err := recover(); err == nil {
			t.Error("Want ServeHTTP to panic, got nothing.")
		}
		// wantTags := map[string]string{
		// 	metrics.LabelPodName:           "pod",
		// 	metrics.LabelContainerName:     "queue-proxy",
		// 	metrics.LabelResponseCode:      "500",
		// 	metrics.LabelResponseCodeClass: "5xx",
		// }
		// wantResource := &resource.Resource{
		// 	Type: "knative_revision",
		// 	Labels: map[string]string{
		// 		metrics.LabelNamespaceName:     "ns",
		// 		metrics.LabelRevisionName:      "rev",
		// 		metrics.LabelServiceName:       "svc",
		// 		metrics.LabelConfigurationName: "cfg",
		// 	},
		// }

		// metricstest.AssertMetric(t, metricstest.IntMetric("app_request_count", 1, wantTags).WithResource(wantResource))
		// metricstest.AssertMetric(t, metricstest.DistributionCountOnlyMetric("app_request_latencies", 1, wantTags).WithResource(wantResource))
	}()
	handler.ServeHTTP(resp, req)
}

func TestAppRequestMetricsHandler(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(mp, baseHandler, breaker)
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	handler.ServeHTTP(resp, req)

	// wantTags := map[string]string{
	// 	metrics.LabelPodName:           "pod",
	// 	metrics.LabelContainerName:     "queue-proxy",
	// 	metrics.LabelResponseCode:      "200",
	// 	metrics.LabelResponseCodeClass: "2xx",
	// }
	// wantResource := &resource.Resource{
	// 	Type: "knative_revision",
	// 	Labels: map[string]string{
	// 		metrics.LabelNamespaceName:     "ns",
	// 		metrics.LabelRevisionName:      "rev",
	// 		metrics.LabelServiceName:       "svc",
	// 		metrics.LabelConfigurationName: "cfg",
	// 	},
	// }

	// metricstest.AssertMetric(t, metricstest.IntMetric("app_request_count", 1, wantTags).WithResource(wantResource))
	// metricstest.AssertMetric(t, metricstest.DistributionCountOnlyMetric("app_request_latencies", 1, wantTags).WithResource(wantResource))

	// A probe request should not be recorded.
	req.Header.Set(netheader.ProbeKey, "activator")
	handler.ServeHTTP(resp, req)
	// metricstest.AssertMetric(t, metricstest.IntMetric("app_request_count", 1, wantTags).WithResource(wantResource))
	// metricstest.AssertMetric(t, metricstest.DistributionCountOnlyMetric("app_request_latencies", 1, wantTags).WithResource(wantResource))
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
