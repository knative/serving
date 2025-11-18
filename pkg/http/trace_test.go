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
	"testing"
)

func TestExtractTraceID(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{{
		name: "W3C Trace Context traceparent",
		headers: map[string]string{
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		},
		want: "4bf92f3577b34da6a3ce929d0e0e4736",
	}, {
		name: "W3C Trace Context with tracestate",
		headers: map[string]string{
			"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
			"tracestate":  "congo=t61rcWkgMzE",
		},
		want: "0af7651916cd43dd8448eb211c80319c",
	}, {
		name: "B3 TraceId (uppercase)",
		headers: map[string]string{
			"X-B3-TraceId": "80f198ee56343ba864fe8b2a57d3eff7",
		},
		want: "80f198ee56343ba864fe8b2a57d3eff7",
	}, {
		name: "B3 Traceid (lowercase 'id')",
		headers: map[string]string{
			"X-B3-Traceid": "463ac35c9f6413ad48485a3953bb6124",
		},
		want: "463ac35c9f6413ad48485a3953bb6124",
	}, {
		name: "W3C Trace Context preferred over B3",
		headers: map[string]string{
			"traceparent":  "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"X-B3-TraceId": "80f198ee56343ba864fe8b2a57d3eff7",
		},
		want: "4bf92f3577b34da6a3ce929d0e0e4736", // W3C takes precedence
	}, {
		name: "B3 short format (8 bytes)",
		headers: map[string]string{
			"X-B3-TraceId": "463ac35c9f6413ad",
		},
		want: "463ac35c9f6413ad",
	}, {
		name: "No trace headers",
		headers: map[string]string{
			"Content-Type": "application/json",
		},
		want: "",
	}, {
		name:    "Empty headers",
		headers: map[string]string{},
		want:    "",
	}, {
		name: "Invalid traceparent format",
		headers: map[string]string{
			"traceparent": "invalid",
		},
		want: "",
	}, {
		name: "Traceparent with only version",
		headers: map[string]string{
			"traceparent": "00",
		},
		want: "",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := make(http.Header)
			for k, v := range tt.headers {
				header.Set(k, v)
			}

			got := ExtractTraceID(header)
			if got != tt.want {
				t.Errorf("ExtractTraceID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkExtractTraceID(b *testing.B) {
	benchmarks := []struct {
		name    string
		headers map[string]string
	}{{
		name: "W3C Trace Context",
		headers: map[string]string{
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		},
	}, {
		name: "B3 TraceId",
		headers: map[string]string{
			"X-B3-TraceId": "80f198ee56343ba864fe8b2a57d3eff7",
		},
	}, {
		name: "Both formats (W3C preferred)",
		headers: map[string]string{
			"traceparent":  "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"X-B3-TraceId": "80f198ee56343ba864fe8b2a57d3eff7",
		},
	}}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			header := make(http.Header)
			for k, v := range bm.headers {
				header.Set(k, v)
			}

			b.ResetTimer()
			for range b.N {
				ExtractTraceID(header)
			}
		})
	}
}
