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

package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTraceIDInRequestLog verifies that trace IDs are properly extracted
// and included in request logs for both W3C Trace Context and B3 formats.
func TestTraceIDInRequestLog(t *testing.T) {
	tests := []struct {
		name            string
		headers         map[string]string
		expectedTraceID string
		description     string
	}{{
		name: "W3C Trace Context (OpenTelemetry)",
		headers: map[string]string{
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		},
		expectedTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		description:     "Should extract trace ID from W3C traceparent header",
	}, {
		name: "B3 Format (Legacy)",
		headers: map[string]string{
			"X-B3-TraceId": "80f198ee56343ba864fe8b2a57d3eff7",
			"X-B3-SpanId":  "00f067aa0ba902b7",
		},
		expectedTraceID: "80f198ee56343ba864fe8b2a57d3eff7",
		description:     "Should extract trace ID from B3 X-B3-TraceId header",
	}, {
		name: "Both formats present (W3C preferred)",
		headers: map[string]string{
			"traceparent":  "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
			"X-B3-TraceId": "80f198ee56343ba864fe8b2a57d3eff7",
		},
		expectedTraceID: "0af7651916cd43dd8448eb211c80319c",
		description:     "Should prefer W3C Trace Context over B3 when both are present",
	}, {
		name:            "No trace headers",
		headers:         map[string]string{},
		expectedTraceID: "",
		description:     "Should return empty string when no trace headers present",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logOutput bytes.Buffer

			// Use the default template format with TraceID field
			template := `{"traceId": "{{.TraceID}}", "status": {{.Response.Code}}}`

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			rev := &RequestLogRevision{
				Name:      "test-rev",
				Namespace: "test-ns",
				Service:   "test-svc",
			}

			logHandler, err := NewRequestLogHandler(
				handler,
				&logOutput,
				template,
				RequestLogTemplateInputGetterFromRevision(rev),
				false,
			)
			if err != nil {
				t.Fatal("Failed to create log handler:", err)
			}

			req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			resp := httptest.NewRecorder()
			logHandler.ServeHTTP(resp, req)

			logLine := logOutput.String()
			if tt.expectedTraceID != "" {
				expectedLog := `"traceId": "` + tt.expectedTraceID + `"`
				if !strings.Contains(logLine, expectedLog) {
					t.Errorf("%s\nExpected log to contain: %s\nGot: %s", tt.description, expectedLog, logLine)
				}
			} else {
				// When no trace ID, should have empty string
				expectedLog := `"traceId": ""`
				if !strings.Contains(logLine, expectedLog) {
					t.Errorf("%s\nExpected log to contain empty traceId\nGot: %s", tt.description, logLine)
				}
			}
		})
	}
}

// TestRequestLogTemplateWithTraceID tests the full request log template
// using the same format as production (config-observability.yaml).
func TestRequestLogTemplateWithTraceID(t *testing.T) {
	var logOutput bytes.Buffer

	// Use the production template format
	template := `{"httpRequest": {"requestMethod": "{{.Request.Method}}", "status": {{.Response.Code}}}, "traceId": "{{.TraceID}}"}`

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rev := &RequestLogRevision{
		Name:      "my-service-abc123",
		Namespace: "default",
		Service:   "my-service",
	}

	logHandler, err := NewRequestLogHandler(
		handler,
		&logOutput,
		template,
		RequestLogTemplateInputGetterFromRevision(rev),
		false,
	)
	if err != nil {
		t.Fatal("Failed to create log handler:", err)
	}

	// Test with W3C Trace Context
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	resp := httptest.NewRecorder()
	logHandler.ServeHTTP(resp, req)

	logLine := logOutput.String()

	// Verify all expected fields are present
	expectedFields := []string{
		`"requestMethod": "POST"`,
		`"status": 200`,
		`"traceId": "4bf92f3577b34da6a3ce929d0e0e4736"`,
	}

	for _, expected := range expectedFields {
		if !strings.Contains(logLine, expected) {
			t.Errorf("Expected log to contain: %s\nGot: %s", expected, logLine)
		}
	}
}
