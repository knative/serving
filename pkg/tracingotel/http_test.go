/*
Copyright 2022 The Knative Authors

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

package tracingotel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	. "knative.dev/serving/pkg/tracingotel"
	"knative.dev/serving/pkg/tracingotel/config"
)

var memoryExporter *tracetest.InMemoryExporter

func TestMain(m *testing.M) {
	memoryExporter = tracetest.NewInMemoryExporter()
	ssp := trace.NewSimpleSpanProcessor(memoryExporter)

	// Initialize the global tracer provider with the in-memory exporter
	// Using a distinct service name for tests to avoid conflicts if any.
	// For tests, use a basic config and nil logger.
	testConfig := &config.Config{
		Backend:    config.None, // No backend for most tests, rely on memoryExporter
		SampleRate: 1.0,         // Sample all traces
		Debug:      true,
	}
	if err := Init("http-test-service", testConfig, nil, ssp); err != nil {
		// Log or handle error appropriately. For tests, panic might be acceptable.
		panic("Failed to initialize OpenTelemetry for tests: " + err.Error())
	}

	// Setup B3 propagation globally for tests, as headers are manually set.
	// This ensures B3 headers are correctly parsed and propagated.
	// The B3 propagator can inject and extract in both single and multi-header formats.
	// By default, it uses multi-header. For single header, use b3.New(b3.WithInjectEncoding(b3.B3SingleHeader)).
	// The tests seem to use X-B3-Traceid, X-B3-Spanid (multi-header).
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, // W3C Trace Context
		b3.New(),                   // B3 Multi-header
	)
	otel.SetTextMapPropagator(prop)

	// Run the tests
	code := m.Run()

	// Shutdown the tracer provider
	if err := Shutdown(context.Background()); err != nil {
		// Log or handle error appropriately
		panic("Failed to shutdown OpenTelemetry for tests: " + err.Error())
	}
	os.Exit(code)
}

type testHandler struct{}

func (th *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("fake"))
}

func TestHTTPSpanMiddleware(t *testing.T) {
	// Ensure memoryExporter is cleared before each test run that uses it.
	memoryExporter.Reset()

	next := &testHandler{}
	// HTTPSpanMiddleware is from the "knative.dev/serving/pkg/tracingotel" package
	middleware := HTTPSpanMiddleware(next)

	// Use httptest.NewRecorder instead of fakeWriter for a more standard library approach
	rr := httptest.NewRecorder()

	req, err := http.NewRequest(http.MethodGet, "http://test.example.com", nil)
	if err != nil {
		t.Fatal("Failed to make fake request:", err)
	}

	// Setting B3 headers for context propagation test
	const expectedTraceIDStr = "821e0d50d931235a5ba3fa42eddddd8f"
	const expectedSpanIDStr = "b3bd5e1c4318c78a" // This will be the parent span ID

	req.Header.Set("X-B3-Traceid", expectedTraceIDStr)
	req.Header.Set("X-B3-Spanid", expectedSpanIDStr)
	req.Header.Set("X-B3-Sampled", "1")

	middleware.ServeHTTP(rr, req)

	// Assert our next handler was called
	if got, want := rr.Body.String(), "fake"; got != want {
		t.Errorf("HTTP Response: got %q, want: %q", got, want)
	}
	if got, want := rr.Code, http.StatusOK; got != want {
		t.Errorf("HTTP Status Code: got %d, want: %d", got, want)
	}

	// Assert spans
	spans := memoryExporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Got %d spans, expected 1: spans = %+v", len(spans), spans)
	}
	span := spans[0]

	// Check TraceID
	// The TraceID in the span should match the one from the incoming B3 header.
	if got := span.SpanContext.TraceID().String(); got != expectedTraceIDStr {
		t.Errorf("span.SpanContext.TraceID() = %s, want %s", got, expectedTraceIDStr)
	}

	// Check Parent SpanID
	// The SpanID from the B3 header should be the parent of the server span created by the middleware.
	if got := span.Parent.SpanID().String(); got != expectedSpanIDStr {
		t.Errorf("span.Parent.SpanID() = %s, want %s", got, expectedSpanIDStr)
	}

	// Check other span attributes (example)
	// Name should be "server" (or whatever otelhttp default is, or what we configured in http.go)
	// The name used in http.go was "server" for otelhttp.NewHandler.
	// The otelhttp spec indicates the span name is typically the route.
	// Let's check the default name given by otelhttp or the one we set ("server").
	// Default for otelhttp is typically "<http.method> <route>" or just "<http.method>".
	// The operation name passed to NewHandler was "server". Let's verify this.
	// Actually, the second argument to NewHandler is operation, which otelhttp uses as span name.
	// In our http.go, we used "server".

	if got, want := span.Name, "server"; got != want {
		t.Errorf("span.Name = %q, want %q", got, want)
	}

	// Check Span Kind - should be server
	if got, want := span.SpanKind, oteltrace.SpanKindServer; got != want {
		t.Errorf("span.SpanKind = %v, want %v", got, want)
	}

	// TODO: Add more assertions as needed, e.g., for attributes like http.method, http.url, etc.
	// For example:
	// attrs := attributesToMap(span.Attributes())
	// if attrs[string(semconv.HTTPMethodKey)] != http.MethodGet {
	//  t.Errorf("...")
	// }
}

func TestHTTPSpanIgnoringPaths(t *testing.T) {
	// Original config setup is no longer needed due to TestMain and OTel Init
	// reporter, co := FakeZipkinExporter()
	// oct := NewOpenCensusTracer(co)
	// ... oct.ApplyConfig ...

	pathsToIgnore := []string{"/readyz"}
	// HTTPSpanIgnoringPaths is from the "knative.dev/serving/pkg/tracingotel" package
	middleware := HTTPSpanIgnoringPaths(pathsToIgnore...)(&testHandler{})

	testCases := []struct {
		name               string
		path               string
		traced             bool
		expectedStatusCode int
	}{{
		name:               "traced path",
		path:               "/",
		traced:             true,
		expectedStatusCode: http.StatusOK,
	}, {
		name:               "ignored path",
		path:               pathsToIgnore[0], // e.g., "/readyz"
		traced:             false,
		expectedStatusCode: http.StatusOK, // Middleware should still call the handler
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			memoryExporter.Reset() // Clear spans before each sub-test

			rr := httptest.NewRecorder()
			u := &url.URL{
				Scheme: "http",
				Host:   "test.example.com",
				Path:   tc.path,
			}
			req, err := http.NewRequest(http.MethodGet, u.String(), nil)
			if err != nil {
				t.Fatal("Failed to make fake request:", err)
			}

			// Set B3 headers to simulate an incoming request with tracing context
			const expectedTraceIDStr = "c0f753ef997a0d5e28a308e8f691db3c"
			const parentSpanIDStr = "a0f09ee0dd547e59"
			req.Header.Set("X-B3-Traceid", expectedTraceIDStr)
			req.Header.Set("X-B3-Spanid", parentSpanIDStr)
			req.Header.Set("X-B3-Sampled", "1")

			middleware.ServeHTTP(rr, req)

			// Assert our next handler was called
			if got, want := rr.Body.String(), "fake"; got != want {
				t.Errorf("HTTP response body: got %q, want: %q", got, want)
			}
			if got, want := rr.Code, tc.expectedStatusCode; got != want {
				t.Errorf("HTTP status code: got %d, want: %d", got, want)
			}

			spans := memoryExporter.GetSpans()
			if tc.traced {
				if len(spans) != 1 {
					t.Fatalf("Got %d spans, expected 1: spans = %+v", len(spans), spans)
				}
				span := spans[0]
				if got := span.SpanContext.TraceID().String(); got != expectedTraceIDStr {
					t.Errorf("span.SpanContext.TraceID() = %s, want %s", got, expectedTraceIDStr)
				}
				if got := span.Parent.SpanID().String(); got != parentSpanIDStr {
					t.Errorf("span.Parent.SpanID() = %s, want %s", got, parentSpanIDStr)
				}
				// Check span name (should be "server" as configured in http.go)
				if got, want := span.Name, "server"; got != want {
					t.Errorf("span.Name = %q, want %q", got, want)
				}
			} else if len(spans) != 0 {
				t.Errorf("Got %d spans, expected 0 for ignored path: spans = %+v", len(spans), spans)
			}
		})
	}
}

// benchWriter is used by BenchmarkSpanMiddleware
type benchWriter struct{}

func (dw *benchWriter) Header() http.Header {
	return http.Header{}
}

func (dw *benchWriter) Write(data []byte) (int, error) {
	return 0, nil // Benchmarking, content doesn't matter
}

func (dw *benchWriter) WriteHeader(statusCode int) {}

func BenchmarkSpanMiddleware(b *testing.B) {
	// OpenCensus specific setup is removed; OTel is configured in TestMain.
	// cfg := config.Config{Backend: config.Zipkin, Debug: true}
	// reporter, co := FakeZipkinExporter()
	// oct := NewOpenCensusTracer(co)
	// b.Cleanup(...)
	// oct.ApplyConfig(&cfg)

	// Ensure memoryExporter is Reset if spans were to be checked, though not typical for benchmarks.
	// For benchmarking tracing overhead, we might not want the exporter to do much work.
	// The current setup with SimpleSpanProcessor and InMemoryExporter will process spans.
	// If this causes too much overhead for the benchmark itself, specific benchmark setup might be needed.
	// For now, assume this is acceptable to ensure tracing path is exercised.
	memoryExporter.Reset() // Resetting in case any state matters or for consistency.

	next := &testHandler{}
	middleware := HTTPSpanMiddleware(next) // Uses the OTel-migrated version

	bw := &benchWriter{}

	req, err := http.NewRequest(http.MethodGet, "http://test.example.com", nil)
	if err != nil {
		b.Fatal("Failed to make fake request:", err)
	}

	// Set B3 headers. While not strictly necessary for the benchmark logic itself
	// (as we don't assert spans here), it makes the benchmark scenario more realistic
	// by including context propagation overhead.
	req.Header.Set("X-B3-Traceid", "721e0d50d93f235a5ba3fa42eddddd8f")
	req.Header.Set("X-B3-Spanid", "c3bd5e1c4318c78a")
	req.Header.Set("X-B3-Sampled", "1")

	b.ResetTimer() // Reset timer after setup

	b.Run("sequential", func(b *testing.B) {
		for range b.N {
			middleware.ServeHTTP(bw, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				middleware.ServeHTTP(bw, req)
			}
		})
	})
}
