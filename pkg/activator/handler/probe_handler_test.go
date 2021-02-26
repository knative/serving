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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	network "knative.dev/networking/pkg"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/queue"
)

func TestProbeHandler(t *testing.T) {
	examples := []struct {
		label          string
		headers        http.Header
		passed         bool
		expectedStatus int
		method         string
	}{{
		label:          "forward a normal POST request",
		headers:        http.Header{},
		passed:         true,
		expectedStatus: http.StatusOK,
		method:         http.MethodPost,
	}, {
		label:          "filter a POST request containing probe header, even if probe is for a different target",
		headers:        http.Header{network.ProbeHeaderName: []string{queue.Name}},
		expectedStatus: http.StatusBadRequest,
		method:         http.MethodPost,
	}, {
		label:          "filter a POST request containing probe header",
		headers:        http.Header{network.ProbeHeaderName: []string{activator.Name}},
		expectedStatus: http.StatusOK,
		method:         http.MethodPost,
	}, {
		label:          "forward a normal GET request",
		headers:        http.Header{},
		passed:         true,
		expectedStatus: http.StatusOK,
		method:         http.MethodGet,
	}, {
		label:          "filter a GET request containing probe header, with wrong target system",
		headers:        http.Header{network.ProbeHeaderName: []string{"not-empty"}},
		expectedStatus: http.StatusBadRequest,
		method:         http.MethodGet,
	}, {
		label:          "filter a GET request containing probe header",
		headers:        http.Header{network.ProbeHeaderName: []string{activator.Name}},
		passed:         false,
		expectedStatus: http.StatusOK,
		method:         http.MethodGet,
	}, {
		label:          "forward a request containing empty retry header",
		headers:        http.Header{network.ProbeHeaderName: []string{""}},
		passed:         true,
		expectedStatus: http.StatusOK,
		method:         http.MethodPost,
	}}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			wasPassed := false
			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wasPassed = true
				w.WriteHeader(http.StatusOK)
			})
			handler := ProbeHandler{NextHandler: baseHandler}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(e.method, "http://example.com", nil)
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

func BenchmarkProbeHandler(b *testing.B) {
	tests := []struct {
		label   string
		headers http.Header
	}{{
		label:   "valid header name",
		headers: http.Header{network.ProbeHeaderName: []string{activator.Name}},
	}, {
		label:   "invalid header name",
		headers: http.Header{network.ProbeHeaderName: []string{"some-other-cool-value"}},
	}, {
		label:   "empty header name",
		headers: http.Header{},
	}}

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := ProbeHandler{NextHandler: baseHandler}
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	for _, test := range tests {
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
