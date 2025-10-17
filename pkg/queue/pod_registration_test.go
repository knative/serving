/*
Copyright 2025 The Knative Authors

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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestSetupTeardown setup/teardown helper for tests
func testSetup(t *testing.T) {
	// No setup needed now that time-based deduplication is removed
	// State-based deduplication happens at caller level (sharedmain.go)
}

// mockActivatorServer simulates an activator pod registration endpoint
func mockActivatorServer(t *testing.T, requests chan<- *http.Request) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != RegistrationEndpoint {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Verify User-Agent
		if r.Header.Get("User-Agent") != "knative-queue-proxy" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Verify Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Read body before sending to channel to preserve it for tests
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		// Replace body with a new reader so tests can read it again
		r.Body = io.NopCloser(bytes.NewReader(body))

		requests <- r

		// Return success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "accepted",
		})
	}))
}

func readPodRegistrationRequest(t *testing.T, r *http.Request) *PodRegistrationRequest {
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}

	var req PodRegistrationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	return &req
}

func TestRegisterPodWithActivator_EmptyURL(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	// Should be no-op with empty URL
	RegisterPodWithActivator("", EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)

	// Give it a moment - should not try to send anything
	time.Sleep(100 * time.Millisecond)

	// If we get here without timeout, the test passed
}

func TestRegisterPodWithActivator_ValidStartupRequest(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	requests := make(chan *http.Request, 1)
	defer close(requests)

	server := mockActivatorServer(t, requests)
	defer server.Close()

	// Register a pod
	RegisterPodWithActivator(server.URL, EventStartup, "test-pod-123", "10.0.0.5", "default", "my-revision", sugaredLogger)

	// Wait for the request
	var req *http.Request
	select {
	case req = <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for registration request")
	}

	// Verify request
	podReq := readPodRegistrationRequest(t, req)

	if podReq.PodName != "test-pod-123" {
		t.Errorf("expected pod name 'test-pod-123', got %q", podReq.PodName)
	}
	if podReq.PodIP != "10.0.0.5" {
		t.Errorf("expected pod IP '10.0.0.5', got %q", podReq.PodIP)
	}
	if podReq.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", podReq.Namespace)
	}
	if podReq.Revision != "my-revision" {
		t.Errorf("expected revision 'my-revision', got %q", podReq.Revision)
	}
	if podReq.EventType != EventStartup {
		t.Errorf("expected event type %q, got %q", EventStartup, podReq.EventType)
	}

	// Verify timestamp is set and recent
	reqTime, err := time.Parse(time.RFC3339Nano, podReq.Timestamp)
	if err != nil {
		t.Errorf("failed to parse timestamp: %v", err)
	}
	if time.Since(reqTime) > 5*time.Second {
		t.Errorf("timestamp seems too old: %v", podReq.Timestamp)
	}
}

func TestRegisterPodWithActivator_ValidReadyRequest(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	requests := make(chan *http.Request, 1)
	defer close(requests)

	server := mockActivatorServer(t, requests)
	defer server.Close()

	// Register a pod as ready
	RegisterPodWithActivator(server.URL, EventReady, "test-pod-456", "10.0.0.6", "kserve", "my-revision-v2", sugaredLogger)

	// Wait for the request
	var req *http.Request
	select {
	case req = <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for registration request")
	}

	podReq := readPodRegistrationRequest(t, req)

	if podReq.EventType != EventReady {
		t.Errorf("expected event type %q, got %q", EventReady, podReq.EventType)
	}
	if podReq.Namespace != "kserve" {
		t.Errorf("expected namespace 'kserve', got %q", podReq.Namespace)
	}
}

func TestRegisterPodWithActivator_UserAgent(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	requests := make(chan *http.Request, 1)
	defer close(requests)

	server := mockActivatorServer(t, requests)
	defer server.Close()

	RegisterPodWithActivator(server.URL, EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)

	var req *http.Request
	select {
	case req = <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for registration request")
	}

	userAgent := req.Header.Get("User-Agent")
	if userAgent != "knative-queue-proxy" {
		t.Errorf("expected User-Agent 'knative-queue-proxy', got %q", userAgent)
	}

	contentType := req.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}
}

func TestRegisterPodWithActivator_Async(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	requests := make(chan *http.Request, 10)
	defer close(requests)

	server := mockActivatorServer(t, requests)
	defer server.Close()

	// Record when RegisterPodWithActivator returns
	start := time.Now()
	RegisterPodWithActivator(server.URL, EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)
	elapsed := time.Since(start)

	// Should return almost immediately (no blocking)
	if elapsed > 100*time.Millisecond {
		t.Errorf("RegisterPodWithActivator should be non-blocking, took %v", elapsed)
	}

	// The actual request should be sent asynchronously
	select {
	case <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for async registration request")
	}
}

func TestRegisterPodWithActivator_MultipleRequests(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	requests := make(chan *http.Request, 10)
	defer close(requests)

	server := mockActivatorServer(t, requests)
	defer server.Close()

	// Send multiple registrations
	for i := range 5 {
		podName := "test-pod-" + string(rune(i))
		podIP := "10.0.0." + string(rune(5+i))

		RegisterPodWithActivator(server.URL, EventStartup, podName, podIP, "default", "my-revision", sugaredLogger)
	}

	// Wait for all requests
	received := 0
	for {
		select {
		case <-requests:
			received++
			if received == 5 {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for %d requests, got %d", 5, received)
		}
	}
}

func TestRegisterPodWithActivator_ConcurrentRequests(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	requests := make(chan *http.Request, 100)
	defer close(requests)

	server := mockActivatorServer(t, requests)
	defer server.Close()

	// Send 100 concurrent registrations
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			podName := "test-pod-" + string(rune(index))
			podIP := "10.0.0." + string(rune((index%250)+5))

			RegisterPodWithActivator(server.URL, EventStartup, podName, podIP, "default", "my-revision", sugaredLogger)
		}(i)
	}

	wg.Wait()

	// Wait for all requests
	received := 0
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-requests:
			received++
			if received == 100 {
				return
			}
		case <-timeout:
			t.Fatalf("timeout waiting for 100 requests, got %d", received)
		}
	}
}

func TestRegisterPodWithActivator_Timeout(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	// Create a server that delays responses beyond the timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than RegistrationTimeout
		time.Sleep(RegistrationTimeout + 1*time.Second)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// This should timeout gracefully and not panic
	RegisterPodWithActivator(server.URL, EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)

	// Wait a bit for async goroutine to complete and handle timeout
	time.Sleep(RegistrationTimeout + 2*time.Second)

	// If we get here without panic, test passed
}

func TestRegisterPodWithActivator_ServerError(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	// Create a server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// This should handle the error gracefully
	RegisterPodWithActivator(server.URL, EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)

	// Wait for async request to complete
	time.Sleep(500 * time.Millisecond)

	// If we get here without panic, test passed
}

func TestRegisterPodWithActivator_InvalidURL(t *testing.T) {
	testSetup(t)
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	// This should handle the invalid URL gracefully
	RegisterPodWithActivator("not-a-valid-url://bad[", EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)

	// Wait for async request to fail gracefully
	time.Sleep(500 * time.Millisecond)

	// If we get here without panic, test passed
}

func TestRegistrationConstants(t *testing.T) {
	// Verify constants are defined correctly
	if RegistrationEndpoint != "/api/v1/pod-registration" {
		t.Errorf("expected endpoint '/api/v1/pod-registration', got %q", RegistrationEndpoint)
	}

	if RegistrationTimeout != 2*time.Second {
		t.Errorf("expected timeout 2s, got %v", RegistrationTimeout)
	}

	if EventStartup != "startup" {
		t.Errorf("expected event type 'startup', got %q", EventStartup)
	}

	if EventReady != "ready" {
		t.Errorf("expected event type 'ready', got %q", EventReady)
	}
}

// Benchmark test for RegisterPodWithActivator
func BenchmarkRegisterPodWithActivator(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	requests := make(chan *http.Request, b.N)
	defer close(requests)

	server := mockActivatorServer(&testing.T{}, requests)
	defer server.Close()

	// Drain requests as they come in
	go func() {
		for range requests {
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		RegisterPodWithActivator(server.URL, EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)
	}

	b.StopTimer()
}

// Benchmark test for request creation time
func BenchmarkRegisterPodWithActivator_Latency(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	sugaredLogger := logger.Sugar()

	counter := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		RegisterPodWithActivator(server.URL, EventStartup, "test-pod", "10.0.0.5", "default", "my-revision", sugaredLogger)
	}

	b.StopTimer()

	// Give async requests time to complete
	time.Sleep(1 * time.Second)
}
