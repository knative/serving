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

package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	ktesting "knative.dev/pkg/logging/testing"
)

func TestHealthHandler(t *testing.T) {
	logger := ktesting.TestLogger(t)
	examples := []struct {
		name           string
		headers        http.Header
		passed         bool
		expectedStatus int
		check          func() error
	}{{
		name:           "forward non-kubelet request",
		headers:        mapToHeader(map[string]string{"User-Agent": "chromium/734.6.5"}),
		passed:         true,
		expectedStatus: http.StatusOK,
	}, {
		name:           "kubelet probe success",
		headers:        mapToHeader(map[string]string{"User-Agent": "kube-probe/something"}),
		passed:         false,
		expectedStatus: http.StatusOK,
		check:          func() error { return nil },
	}, {
		name:           "kubelet probe failure",
		headers:        mapToHeader(map[string]string{"User-Agent": "kube-probe/something"}),
		passed:         false,
		expectedStatus: http.StatusInternalServerError,
		check:          func() error { return errors.New("not ready") },
	}}

	for _, e := range examples {
		t.Run(e.name, func(t *testing.T) {
			wasPassed := false
			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wasPassed = true
				w.WriteHeader(http.StatusOK)
			})
			handler := HealthHandler{HealthCheck: e.check, NextHandler: baseHandler, Logger: logger}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			req.Header = e.headers

			handler.ServeHTTP(resp, req)

			if wasPassed != e.passed {
				if !e.passed {
					t.Error("Request got passed to the next handler unexpectedly")
				} else {
					t.Error("Request was not passed to the next handler as expected")
				}
			}

			if resp.Code != e.expectedStatus {
				t.Errorf("Unexpected response status. Want %d, got %d", e.expectedStatus, resp.Code)
			}
		})
	}
}

func BenchmarkHealthHandler(b *testing.B) {
	tests := []struct {
		label   string
		headers http.Header
		check   func() error
	}{{
		label:   "forward non-kubelet request",
		headers: mapToHeader(map[string]string{"User-Agent": "chromium/734.6.5"}),
		check:   func() error { return nil },
	}, {
		label:   "kubelet probe success",
		headers: mapToHeader(map[string]string{"User-Agent": "kube-probe/something"}),
		check:   func() error { return nil },
	}, {
		label:   "kubelet probe failure",
		headers: mapToHeader(map[string]string{"User-Agent": "kube-probe/something"}),
		check:   func() error { return errors.New("not ready") },
	}}

	logger := ktesting.TestLogger(b)
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	for _, test := range tests {
		handler := HealthHandler{HealthCheck: test.check, NextHandler: baseHandler, Logger: logger}
		req.Header = test.headers
		b.Run(fmt.Sprintf("%s-sequential", test.label), func(b *testing.B) {
			resp := httptest.NewRecorder()
			for j := 0; j < b.N; j++ {
				handler.ServeHTTP(resp, req)
			}
		})

		b.Run(fmt.Sprintf("%s-parallel", test.label), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				resp := httptest.NewRecorder()
				for pb.Next() {
					handler.ServeHTTP(resp, req)
				}
			})
		})
	}
}
