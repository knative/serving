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

	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"
	"knative.dev/serving/pkg/network"
)

const targetURI = "http://example.com"

func TestNewRequestMetricsHandlerFailure(t *testing.T) {
	if _, err := NewRequestMetricsHandler(nil /*next*/, "shøüld fail", "a", "b", "c", "d"); err == nil {
		t.Error("Should get error when StatsReporter is empty")
	}
}

func TestRequestMetricsHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler, err := NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod")
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	handler.ServeHTTP(resp, req)

	wantTags := map[string]string{
		metricskey.LabelNamespaceName:     "ns",
		metricskey.LabelRevisionName:      "rev",
		metricskey.LabelServiceName:       "svc",
		metricskey.LabelConfigurationName: "cfg",
		"pod_name":                        "pod",
		"container_name":                  "queue-proxy",
		metricskey.LabelResponseCode:      "200",
		metricskey.LabelResponseCodeClass: "2xx",
	}

	metricstest.CheckCountData(t, "request_count", wantTags, 1)
	metricstest.CheckDistributionCount(t, "request_latencies", wantTags, 1) // Dummy latency range.

	// A probe request should not be recorded.
	req.Header.Set(network.ProbeHeaderName, "activator")
	handler.ServeHTTP(resp, req)
	metricstest.CheckCountData(t, "request_count", wantTags, 1)
	metricstest.CheckDistributionCount(t, "request_latencies", wantTags, 1)
}

func reset() {
	metricstest.Unregister(
		requestCountM.Name(), appRequestCountM.Name(),
		responseTimeInMsecM.Name(), appResponseTimeInMsecM.Name(),
		queueDepthM.Name())
}

func TestRequestMetricsHandlerPanickingHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no!")
	})
	handler, err := NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod")
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	defer func() {
		if err := recover(); err == nil {
			t.Error("Want ServeHTTP to panic, got nothing.")
		}
		wantTags := map[string]string{
			metricskey.LabelNamespaceName:     "ns",
			metricskey.LabelRevisionName:      "rev",
			metricskey.LabelServiceName:       "svc",
			metricskey.LabelConfigurationName: "cfg",
			"pod_name":                        "pod",
			"container_name":                  "queue-proxy",
			metricskey.LabelResponseCode:      "500",
			metricskey.LabelResponseCodeClass: "5xx",
		}
		metricstest.CheckCountData(t, "request_count", wantTags, 1)
		metricstest.CheckDistributionCount(t, "request_latencies", wantTags, 1) // Dummy latency range.
	}()
	handler.ServeHTTP(resp, req)
}

func BenchmarkNewRequestMetricsHandler(b *testing.B) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(baseHandler, breaker, "test-ns",
		"test-svc", "test-cfg", "test-rev", "test-pod")
	if err != nil {
		b.Fatalf("failed to create request metric handler: %v", err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, nil)

	b.Run("sequential", func(b *testing.B) {
		for j := 0; j < b.N; j++ {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}

func TestAppRequestMetricsHandlerPanickingHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no!")
	})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(baseHandler, breaker,
		"ns", "svc", "cfg", "rev", "pod")
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	defer func() {
		if err := recover(); err == nil {
			t.Error("Want ServeHTTP to panic, got nothing.")
		}
		wantTags := map[string]string{
			metricskey.LabelNamespaceName:     "ns",
			metricskey.LabelRevisionName:      "rev",
			metricskey.LabelServiceName:       "svc",
			metricskey.LabelConfigurationName: "cfg",
			"pod_name":                        "pod",
			"container_name":                  "queue-proxy",
			metricskey.LabelResponseCode:      "500",
			metricskey.LabelResponseCodeClass: "5xx",
		}
		metricstest.CheckCountData(t, "app_request_count", wantTags, 1)
		metricstest.CheckDistributionCount(t, "app_request_latencies", wantTags, 1) // Dummy latency range.
	}()
	handler.ServeHTTP(resp, req)
}

func TestAppRequestMetricsHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(baseHandler, breaker,
		"ns", "svc", "cfg", "rev", "pod")
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	handler.ServeHTTP(resp, req)

	wantTags := map[string]string{
		metricskey.LabelNamespaceName:     "ns",
		metricskey.LabelRevisionName:      "rev",
		metricskey.LabelServiceName:       "svc",
		metricskey.LabelConfigurationName: "cfg",
		"pod_name":                        "pod",
		"container_name":                  "queue-proxy",
		metricskey.LabelResponseCode:      "200",
		metricskey.LabelResponseCodeClass: "2xx",
	}

	metricstest.CheckCountData(t, "app_request_count", wantTags, 1)
	metricstest.CheckDistributionCount(t, "app_request_latencies", wantTags, 1) // Dummy latency range.

	// A probe request should not be recorded.
	req.Header.Set(network.ProbeHeaderName, "activator")
	handler.ServeHTTP(resp, req)
	metricstest.CheckCountData(t, "app_request_count", wantTags, 1)
	metricstest.CheckDistributionCount(t, "app_request_latencies", wantTags, 1)
}
