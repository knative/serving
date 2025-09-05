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
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
	pkghandler "knative.dev/pkg/network/handlers"
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
