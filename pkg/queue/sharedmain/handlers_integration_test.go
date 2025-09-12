/*
Copyright 2024 The Knative Authors

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

package sharedmain

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"

	netstats "knative.dev/networking/pkg/http/stats"
	"knative.dev/pkg/network"
	"knative.dev/serving/pkg/observability"
)

func TestMainHandlerWithPendingRequests(t *testing.T) {
	logger := zap.NewNop().Sugar()
	tp := trace.NewTracerProvider()
	mp := metric.NewMeterProvider()

	// Create a backend server to proxy to
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate some processing time
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response"))
	}))
	defer backend.Close()

	// Extract port from backend URL
	_, port, _ := net.SplitHostPort(backend.Listener.Addr().String())

	env := config{
		ContainerConcurrency:   10,
		QueueServingPort:       "8080",
		QueueServingTLSPort:    "8443",
		UserPort:               port,
		RevisionTimeoutSeconds: 300,
		ServingLoggingConfig:   "",
		ServingLoggingLevel:    "info",
		Observability:          *observability.DefaultConfig(),
		Env: Env{
			ServingNamespace:     "test-namespace",
			ServingConfiguration: "test-config",
			ServingRevision:      "test-revision",
			ServingPod:           "test-pod",
			ServingPodIP:         "10.0.0.1",
		},
	}

	transport := buildTransport(env, tp, mp)
	prober := func() bool { return true }
	stats := netstats.NewRequestStats(time.Now())
	pendingRequests := atomic.Int32{}

	handler, drainer := mainHandler(env, transport, prober, stats, logger, mp, tp, &pendingRequests)

	t.Run("tracks pending requests correctly", func(t *testing.T) {
		// Make a regular request
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "test.example.com"

		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}()

		// Give the request time to start
		time.Sleep(10 * time.Millisecond)

		// Check that pending request counter was incremented
		count := pendingRequests.Load()
		if count != 1 {
			t.Errorf("Expected 1 pending request, got %d", count)
		}

		wg.Wait()

		// Check that counter was decremented after completion
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected 0 pending requests after completion, got %d", pendingRequests.Load())
		}
	})

	t.Run("does not track probe requests", func(t *testing.T) {
		// Make a probe request
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(network.ProbeHeaderName, network.ProbeHeaderValue)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Check that pending request counter was not incremented
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected 0 pending requests for probe, got %d", pendingRequests.Load())
		}
	})

	t.Run("handles concurrent requests", func(t *testing.T) {
		numRequests := 5
		var wg sync.WaitGroup
		wg.Add(numRequests)

		for i := 0; i < numRequests; i++ {
			go func(i int) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Host = "test.example.com"
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
			}(i)
		}

		// Give requests time to start
		time.Sleep(20 * time.Millisecond)

		// Check that multiple requests are being tracked
		count := pendingRequests.Load()
		if count <= 0 || count > int32(numRequests) {
			t.Errorf("Expected pending requests between 1 and %d, got %d", numRequests, count)
		}

		wg.Wait()

		// Check that all requests completed
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected 0 pending requests after all completed, got %d", pendingRequests.Load())
		}
	})

	t.Run("drainer integration", func(t *testing.T) {
		// Start a long-running request
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "test.example.com"

		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}()

		// Give request time to start
		time.Sleep(10 * time.Millisecond)

		// Verify request is being tracked
		if pendingRequests.Load() != 1 {
			t.Errorf("Expected 1 pending request before drain, got %d", pendingRequests.Load())
		}

		// Call drain
		drainer.Drain()

		// Wait for request to complete
		wg.Wait()

		// Verify counter is back to 0
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected 0 pending requests after drain, got %d", pendingRequests.Load())
		}
	})
}

func TestBuildBreaker(t *testing.T) {
	logger := zap.NewNop().Sugar()

	t.Run("returns nil for unlimited concurrency", func(t *testing.T) {
		env := config{
			ContainerConcurrency: 0,
		}
		breaker := buildBreaker(logger, env)
		if breaker != nil {
			t.Error("Expected nil breaker for unlimited concurrency")
		}
	})

	t.Run("creates breaker with correct params", func(t *testing.T) {
		env := config{
			ContainerConcurrency: 10,
		}
		breaker := buildBreaker(logger, env)
		if breaker == nil {
			t.Fatal("Expected non-nil breaker")
		}
		// The breaker should be configured with QueueDepth = 10 * ContainerConcurrency
		// and MaxConcurrency = ContainerConcurrency
	})
}

func TestBuildProbe(t *testing.T) {
	logger := zap.NewNop().Sugar()

	t.Run("creates probe without HTTP2 auto-detection", func(t *testing.T) {
		encodedProbe := `{"httpGet":{"path":"/health","port":8080}}`
		probe := buildProbe(logger, encodedProbe, false, false)
		if probe == nil {
			t.Fatal("Expected non-nil probe")
		}
	})

	t.Run("creates probe with HTTP2 auto-detection", func(t *testing.T) {
		encodedProbe := `{"httpGet":{"path":"/health","port":8080}}`
		probe := buildProbe(logger, encodedProbe, true, false)
		if probe == nil {
			t.Fatal("Expected non-nil probe")
		}
	})
}

func TestExists(t *testing.T) {
	logger := zap.NewNop().Sugar()

	t.Run("returns true for existing file", func(t *testing.T) {
		// Create a temporary file
		tmpfile, err := os.CreateTemp("", "test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpfile.Name())

		if !exists(logger, tmpfile.Name()) {
			t.Error("Expected true for existing file")
		}
	})

	t.Run("returns false for non-existent file", func(t *testing.T) {
		if exists(logger, "/non/existent/file/path") {
			t.Error("Expected false for non-existent file")
		}
	})
}