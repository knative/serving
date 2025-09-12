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
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"knative.dev/pkg/network"
	pkghandler "knative.dev/pkg/network/handlers"
	"knative.dev/serving/pkg/queue"
)

func TestDrainCompleteEndpoint(t *testing.T) {
	logger := zap.NewNop().Sugar()
	drainer := &pkghandler.Drainer{}

	t.Run("returns 200 when no pending requests", func(t *testing.T) {
		pendingRequests := atomic.Int32{}
		pendingRequests.Store(0)

		handler := adminHandler(context.Background(), logger, drainer, &pendingRequests)

		req := httptest.NewRequest(http.MethodGet, "/drain-complete", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "drained" {
			t.Errorf("Expected body 'drained', got %s", w.Body.String())
		}
	})

	t.Run("returns 503 when requests are pending", func(t *testing.T) {
		pendingRequests := atomic.Int32{}
		pendingRequests.Store(5)

		handler := adminHandler(context.Background(), logger, drainer, &pendingRequests)

		req := httptest.NewRequest(http.MethodGet, "/drain-complete", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status 503, got %d", w.Code)
		}
		if w.Body.String() != "pending requests: 5" {
			t.Errorf("Expected body 'pending requests: 5', got %s", w.Body.String())
		}
	})
}

func TestRequestQueueDrainHandler(t *testing.T) {
	logger := zap.NewNop().Sugar()

	t.Run("handles drain request when context is done", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		drainer := &pkghandler.Drainer{
			QuietPeriod: 100 * time.Millisecond,
			Inner: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		}
		pendingRequests := atomic.Int32{}

		handler := adminHandler(ctx, logger, drainer, &pendingRequests)

		// Cancel context to simulate TERM signal
		cancel()

		req := httptest.NewRequest(http.MethodPost, queue.RequestQueueDrainPath, nil)
		w := httptest.NewRecorder()

		// This should call drainer.Drain() and return immediately
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Verify the drainer is in draining state by sending a probe
		probeReq := httptest.NewRequest(http.MethodGet, "/", nil)
		probeReq.Header.Set("User-Agent", "kube-probe/1.0")
		probeW := httptest.NewRecorder()
		drainer.ServeHTTP(probeW, probeReq)

		// Should return 503 because drainer is draining
		if probeW.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected probe to return 503 during drain, got %d", probeW.Code)
		}
	})

	t.Run("resets drainer after timeout when context not done", func(t *testing.T) {
		ctx := context.Background()
		drainer := &pkghandler.Drainer{
			QuietPeriod: 100 * time.Millisecond,
			Inner: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		}
		pendingRequests := atomic.Int32{}

		handler := adminHandler(ctx, logger, drainer, &pendingRequests)

		req := httptest.NewRequest(http.MethodPost, queue.RequestQueueDrainPath, nil)
		w := httptest.NewRecorder()

		// Start the drain in a goroutine since Drain() blocks
		done := make(chan bool)
		go func() {
			handler.ServeHTTP(w, req)
			done <- true
		}()

		// Give it time to start draining
		time.Sleep(50 * time.Millisecond)

		// Check that drainer is draining
		probeReq := httptest.NewRequest(http.MethodGet, "/", nil)
		probeReq.Header.Set("User-Agent", "kube-probe/1.0")
		probeW := httptest.NewRecorder()
		drainer.ServeHTTP(probeW, probeReq)

		if probeW.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected probe to return 503 during drain, got %d", probeW.Code)
		}

		// Wait for the reset to happen (after 1 second)
		time.Sleep(1100 * time.Millisecond)

		// Check that drainer has been reset and is no longer draining
		probeReq2 := httptest.NewRequest(http.MethodGet, "/", nil)
		probeReq2.Header.Set("User-Agent", "kube-probe/1.0")
		probeW2 := httptest.NewRecorder()
		drainer.ServeHTTP(probeW2, probeReq2)

		// Should return 200 because drainer was reset
		if probeW2.Code != http.StatusOK {
			t.Errorf("Expected probe to return 200 after reset, got %d", probeW2.Code)
		}

		// Clean up
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Handler did not complete in time")
		}
	})
}

func TestWithRequestCounter(t *testing.T) {
	pendingRequests := atomic.Int32{}

	// Create a test handler that we'll wrap
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep briefly to ensure counter is incremented
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := withRequestCounter(baseHandler, &pendingRequests)

	t.Run("increments counter for regular requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			wrappedHandler.ServeHTTP(w, req)
		}()

		// Give the request time to start
		time.Sleep(5 * time.Millisecond)

		// Check that counter was incremented
		if pendingRequests.Load() != 1 {
			t.Errorf("Expected pending requests to be 1, got %d", pendingRequests.Load())
		}

		wg.Wait()

		// Check that counter was decremented after request completed
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected pending requests to be 0 after completion, got %d", pendingRequests.Load())
		}
	})

	t.Run("skips counter for probe requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(network.ProbeHeaderName, network.ProbeHeaderValue)
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		// Check that counter was not incremented
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected pending requests to remain 0 for probe, got %d", pendingRequests.Load())
		}
	})

	t.Run("skips counter for kube-probe requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("User-Agent", "kube-probe/1.27")
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		// Check that counter was not incremented
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected pending requests to remain 0 for kube-probe, got %d", pendingRequests.Load())
		}
	})

	t.Run("handles concurrent requests correctly", func(t *testing.T) {
		// Reset counter
		pendingRequests.Store(0)

		numRequests := 10
		var wg sync.WaitGroup
		wg.Add(numRequests)

		for range numRequests {
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				w := httptest.NewRecorder()
				wrappedHandler.ServeHTTP(w, req)
			}()
		}

		// Give requests time to start
		time.Sleep(5 * time.Millisecond)

		// Check that all requests are being tracked
		count := pendingRequests.Load()
		if count <= 0 || count > int32(numRequests) {
			t.Errorf("Expected pending requests to be between 1 and %d, got %d", numRequests, count)
		}

		wg.Wait()

		// Check that counter returned to 0
		if pendingRequests.Load() != 0 {
			t.Errorf("Expected pending requests to be 0 after all completed, got %d", pendingRequests.Load())
		}
	})
}

func TestWithFullDuplex(t *testing.T) {
	logger := zap.NewNop().Sugar()

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("passes through when disabled", func(t *testing.T) {
		wrappedHandler := withFullDuplex(baseHandler, false, logger)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("enables full duplex when configured", func(t *testing.T) {
		wrappedHandler := withFullDuplex(baseHandler, true, logger)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}
