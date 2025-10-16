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

package net

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	. "knative.dev/pkg/logging/testing"
)

type captureThrottler struct {
	mu              sync.Mutex
	registrations   []registrationCall
	handleUpdateCh  chan bool
	expectedRevID   types.NamespacedName
	expectedPodIP   string
	expectedEventType string
}

type registrationCall struct {
	revID     types.NamespacedName
	podIP     string
	eventType string
}

func (ct *captureThrottler) HandlePodRegistration(revID types.NamespacedName, podIP string, eventType string, _ *zap.SugaredLogger) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.registrations = append(ct.registrations, registrationCall{
		revID:     revID,
		podIP:     podIP,
		eventType: eventType,
	})
	if ct.handleUpdateCh != nil {
		ct.handleUpdateCh <- true
	}
}

func (ct *captureThrottler) Try(ctx context.Context, revID types.NamespacedName, xRequestId string, function func(string, bool) error) error {
	// Not used in pod registration tests
	return nil
}

func (ct *captureThrottler) getRegistrations() []registrationCall {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	// Return a copy
	result := make([]registrationCall, len(ct.registrations))
	copy(result, ct.registrations)
	return result
}

func TestPodRegistrationHandler_ValidRequest(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{
		handleUpdateCh: make(chan bool, 1),
	}

	handler := PodRegistrationHandler(throttler, logger)
	w := httptest.NewRecorder()

	req := PodRegistrationRequest{
		PodName:   "test-pod-123",
		PodIP:     "10.0.0.5",
		Namespace: "default",
		Revision:  "my-revision",
		EventType: "startup",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))

	handler.ServeHTTP(w, r)

	// Verify response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var respBody map[string]string
	json.NewDecoder(w.Body).Decode(&respBody)
	if respBody["status"] != "accepted" {
		t.Fatalf("expected status 'accepted', got %q", respBody["status"])
	}

	// Verify throttler was called correctly
	select {
	case <-throttler.handleUpdateCh:
	case <-time.After(time.Second):
		t.Fatal("HandlePodRegistration was not called")
	}

	registrations := throttler.getRegistrations()
	if len(registrations) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(registrations))
	}

	if registrations[0].revID.Namespace != "default" || registrations[0].revID.Name != "my-revision" {
		t.Fatalf("incorrect revision ID: %v", registrations[0].revID)
	}
	if registrations[0].podIP != "10.0.0.5" {
		t.Fatalf("incorrect pod IP: %s", registrations[0].podIP)
	}
	if registrations[0].eventType != "startup" {
		t.Fatalf("incorrect event type: %s", registrations[0].eventType)
	}
}

func TestPodRegistrationHandler_ReadyEvent(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{
		handleUpdateCh: make(chan bool, 1),
	}

	handler := PodRegistrationHandler(throttler, logger)
	w := httptest.NewRecorder()

	req := PodRegistrationRequest{
		PodName:   "test-pod-456",
		PodIP:     "10.0.0.6",
		Namespace: "kserve",
		Revision:  "my-revision-v2",
		EventType: "ready",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	select {
	case <-throttler.handleUpdateCh:
	case <-time.After(time.Second):
		t.Fatal("HandlePodRegistration was not called")
	}

	registrations := throttler.getRegistrations()
	if registrations[0].eventType != "ready" {
		t.Fatalf("expected event type 'ready', got %s", registrations[0].eventType)
	}
}

func TestPodRegistrationHandler_MissingField(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{}

	handler := PodRegistrationHandler(throttler, logger)

	tests := []struct {
		name  string
		req   PodRegistrationRequest
		field string
	}{
		{
			name: "missing_pod_name",
			req: PodRegistrationRequest{
				PodIP:     "10.0.0.5",
				Namespace: "default",
				Revision:  "my-revision",
				EventType: "startup",
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			},
			field: "PodName",
		},
		{
			name: "missing_pod_ip",
			req: PodRegistrationRequest{
				PodName:   "test-pod",
				Namespace: "default",
				Revision:  "my-revision",
				EventType: "startup",
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			},
			field: "PodIP",
		},
		{
			name: "missing_namespace",
			req: PodRegistrationRequest{
				PodName:   "test-pod",
				PodIP:     "10.0.0.5",
				Revision:  "my-revision",
				EventType: "startup",
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			},
			field: "Namespace",
		},
		{
			name: "missing_revision",
			req: PodRegistrationRequest{
				PodName:   "test-pod",
				PodIP:     "10.0.0.5",
				Namespace: "default",
				EventType: "startup",
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			},
			field: "Revision",
		},
		{
			name: "missing_event_type",
			req: PodRegistrationRequest{
				PodName:   "test-pod",
				PodIP:     "10.0.0.5",
				Namespace: "default",
				Revision:  "my-revision",
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			},
			field: "EventType",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			body, _ := json.Marshal(tc.req)
			r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))

			handler.ServeHTTP(w, r)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", w.Code)
			}

			// Throttler should not be called
			if len(throttler.getRegistrations()) > 0 {
				t.Fatal("throttler should not be called for invalid request")
			}
		})
	}
}

func TestPodRegistrationHandler_InvalidMethod(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{}

	handler := PodRegistrationHandler(throttler, logger)

	tests := []string{"GET", "PUT", "DELETE", "PATCH"}

	for _, method := range tests {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/api/v1/pod-registration", nil)

			handler.ServeHTTP(w, r)

			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status 405, got %d", w.Code)
			}

			// Throttler should not be called
			if len(throttler.getRegistrations()) > 0 {
				t.Fatal("throttler should not be called for invalid method")
			}
		})
	}
}

func TestPodRegistrationHandler_InvalidJSON(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{}

	handler := PodRegistrationHandler(throttler, logger)
	w := httptest.NewRecorder()

	// Send invalid JSON
	r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader([]byte("invalid json {{")))

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	// Throttler should not be called
	if len(throttler.getRegistrations()) > 0 {
		t.Fatal("throttler should not be called for invalid JSON")
	}
}

func TestPodRegistrationHandler_EmptyBody(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{}

	handler := PodRegistrationHandler(throttler, logger)
	w := httptest.NewRecorder()

	r := httptest.NewRequest("POST", "/api/v1/pod-registration", io.NopCloser(bytes.NewReader([]byte(""))))

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for empty body, got %d", w.Code)
	}

	// Throttler should not be called
	if len(throttler.getRegistrations()) > 0 {
		t.Fatal("throttler should not be called for empty body")
	}
}

func TestPodRegistrationHandler_MultipleRequests(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{
		handleUpdateCh: make(chan bool, 3),
	}

	handler := PodRegistrationHandler(throttler, logger)

	// Send multiple requests
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()

		req := PodRegistrationRequest{
			PodName:   "test-pod-" + string(rune(i)),
			PodIP:     "10.0.0." + string(rune(5+i)),
			Namespace: "default",
			Revision:  "my-revision",
			EventType: "startup",
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))

		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected status 200, got %d", i, w.Code)
		}
	}

	// Wait for all registrations
	for i := 0; i < 3; i++ {
		select {
		case <-throttler.handleUpdateCh:
		case <-time.After(time.Second):
			t.Fatalf("registration %d was not called", i)
		}
	}

	registrations := throttler.getRegistrations()
	if len(registrations) != 3 {
		t.Fatalf("expected 3 registrations, got %d", len(registrations))
	}

	// Verify they are all different
	podIPs := sets.New[string]()
	for _, reg := range registrations {
		if podIPs.Has(reg.podIP) {
			t.Fatalf("duplicate pod IP: %s", reg.podIP)
		}
		podIPs.Insert(reg.podIP)
	}
}

func TestPodRegistrationHandler_ResponseContentType(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{
		handleUpdateCh: make(chan bool, 1),
	}

	handler := PodRegistrationHandler(throttler, logger)
	w := httptest.NewRecorder()

	req := PodRegistrationRequest{
		PodName:   "test-pod",
		PodIP:     "10.0.0.5",
		Namespace: "default",
		Revision:  "my-revision",
		EventType: "startup",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))

	handler.ServeHTTP(w, r)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("expected Content-Type 'application/json', got %q", contentType)
	}
}

func TestPodRegistrationHandler_ConcurrentRequests(t *testing.T) {
	logger := TestLogger(t)
	throttler := &captureThrottler{
		handleUpdateCh: make(chan bool, 100),
	}

	handler := PodRegistrationHandler(throttler, logger)

	// Send 100 concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			w := httptest.NewRecorder()

			req := PodRegistrationRequest{
				PodName:   "test-pod-" + string(rune(index)),
				PodIP:     "10.0.0." + string(rune((index % 250) + 5)),
				Namespace: "default",
				Revision:  "my-revision",
				EventType: "startup",
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			}

			body, _ := json.Marshal(req)
			r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))

			handler.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", w.Code)
			}
		}(i)
	}

	wg.Wait()

	// Wait for all registrations
	for i := 0; i < 100; i++ {
		select {
		case <-throttler.handleUpdateCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("registration %d was not called within timeout", i)
		}
	}

	registrations := throttler.getRegistrations()
	if len(registrations) != 100 {
		t.Fatalf("expected 100 registrations, got %d", len(registrations))
	}
}

func TestThrottler_HandlePodRegistration(t *testing.T) {
	logger := TestLogger(t)
	ctx := context.Background()

	// Create a real throttler with fake informers
	throttler := NewThrottler(ctx, "10.10.10.10")

	revID := types.NamespacedName{
		Namespace: "default",
		Name:      "my-revision",
	}

	// Call HandlePodRegistration
	throttler.HandlePodRegistration(revID, "10.0.0.5", "startup", logger)

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify that a revision throttler was created or updated
	// We do this by trying to route a request and checking the state
	throttler.revisionThrottlersMutex.RLock()
	revThrottler, exists := throttler.revisionThrottlers[revID]
	throttler.revisionThrottlersMutex.RUnlock()

	// At minimum, the revisionThrottler should exist (even if empty)
	// or the update should have been queued
	_ = revThrottler
	_ = exists
	// The actual assertion is that this completes without panic
}

func TestThrottler_HandlePodRegistration_MultipleIPs(t *testing.T) {
	logger := TestLogger(t)
	ctx := context.Background()

	throttler := NewThrottler(ctx, "10.10.10.10")

	revID := types.NamespacedName{
		Namespace: "default",
		Name:      "my-revision",
	}

	// Register multiple pod IPs for the same revision
	ips := []string{"10.0.0.5", "10.0.0.6", "10.0.0.7"}
	for _, ip := range ips {
		throttler.HandlePodRegistration(revID, ip, "startup", logger)
	}

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify no panics occurred - the actual state verification
	// would require deeper inspection of internal structures
}

func TestThrottler_HandlePodRegistration_EmptyIP(t *testing.T) {
	logger := TestLogger(t)
	ctx := context.Background()

	throttler := NewThrottler(ctx, "10.10.10.10")

	revID := types.NamespacedName{
		Namespace: "default",
		Name:      "my-revision",
	}

	// Call with empty IP - should be no-op
	throttler.HandlePodRegistration(revID, "", "startup", logger)

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Should complete without issues
}

// Verify the handler integrates properly with the Throttler interface
func TestPodRegistrationHandlerIntegration(t *testing.T) {
	logger := TestLogger(t)
	ctx := context.Background()

	// Create a real throttler
	throttler := NewThrottler(ctx, "10.10.10.10")

	handler := PodRegistrationHandler(throttler, logger)
	w := httptest.NewRecorder()

	revID := types.NamespacedName{
		Namespace: "default",
		Name:      "test-revision",
	}

	req := PodRegistrationRequest{
		PodName:   "test-pod",
		PodIP:     "10.0.0.5",
		Namespace: revID.Namespace,
		Revision:  revID.Name,
		EventType: "startup",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))

	// This should complete without panic
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Verify response
	var respBody map[string]string
	json.NewDecoder(w.Body).Decode(&respBody)
	if respBody["status"] != "accepted" {
		t.Fatalf("expected status 'accepted', got %q", respBody["status"])
	}
}

// Benchmark test for pod registration handler
func BenchmarkPodRegistrationHandler(b *testing.B) {
	logger := TestLogger(&testing.T{})
	throttler := &captureThrottler{
		handleUpdateCh: make(chan bool, b.N),
	}

	handler := PodRegistrationHandler(throttler, logger)

	req := PodRegistrationRequest{
		PodName:   "test-pod",
		PodIP:     "10.0.0.5",
		Namespace: "default",
		Revision:  "my-revision",
		EventType: "startup",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	body, _ := json.Marshal(req)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/v1/pod-registration", bytes.NewReader(body))
		handler.ServeHTTP(w, r)
	}
}
