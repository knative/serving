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
	"net/http"
	"strings"
)

// ExtractTraceID extracts the trace ID from the request headers.
// It supports both W3C Trace Context (traceparent) and B3 (X-B3-TraceId) formats.
func ExtractTraceID(h http.Header) string {
	if traceparent := h.Get("Traceparent"); traceparent != "" {
		parts := strings.SplitN(traceparent, "-", 3)
		if len(parts) >= 2 {
			return parts[1]
		}
	}

	if b3TraceID := h.Get("X-B3-Traceid"); b3TraceID != "" {
		return b3TraceID
	}

	return ""
}
