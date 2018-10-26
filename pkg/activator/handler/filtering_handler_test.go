/*
Copyright 2018 The Knative Authors
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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knative/serving/pkg/activator"
)

func TestFilteringHandler(t *testing.T) {
	examples := []struct {
		label          string
		headers        http.Header
		passed         bool
		expectedStatus int
	}{{
		label:          "forward a normal request",
		headers:        http.Header{},
		passed:         true,
		expectedStatus: http.StatusOK,
	},
		{
			label:          "filter a request containing retry header",
			headers:        mapToHeader(map[string]string{activator.RequestCountHTTPHeader: "4"}),
			passed:         false,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			label:          "forward a request containing empty retry header",
			headers:        mapToHeader(map[string]string{activator.RequestCountHTTPHeader: ""}),
			passed:         true,
			expectedStatus: http.StatusOK,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			wasPassed := false
			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wasPassed = true
				w.WriteHeader(http.StatusOK)
			})
			handler := FilteringHandler{NextHandler: baseHandler}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "http://example.com", nil)
			req.Header = e.headers

			handler.ServeHTTP(resp, req)

			if wasPassed != e.passed {
				if !e.passed {
					t.Errorf("Request got passed to the next handler unexpectedly")
				} else {
					t.Errorf("Request was not passed to the next handler as expected")
				}
			}

			if resp.Code != e.expectedStatus {
				t.Errorf("Unexpected response status. Want %d, got %d", e.expectedStatus, resp.Code)
			}
		})
	}
}

func mapToHeader(m map[string]string) http.Header {
	h := http.Header{}
	for k, v := range m {
		h.Add(k, v)
	}
	return h
}
