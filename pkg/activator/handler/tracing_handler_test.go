/*
Copyright 2021 The Knative Authors

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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/logging"
	rtesting "knative.dev/pkg/reconciler/testing"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	"knative.dev/serving/pkg/tracingotel"
	"knative.dev/serving/pkg/tracingotel/config"
)

func TestTracingHandler(t *testing.T) {
	tests := []struct {
		name           string
		tracingEnabled bool
	}{{
		name:           "enabled",
		tracingEnabled: true,
	}, {
		name:           "disabled",
		tracingEnabled: false,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()
			logger := logging.FromContext(ctx)

			// Create a simple handler that doesn't do any internal tracing
			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler := NewTracingHandler(baseHandler)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			const traceID = "821e0d50d931235a5ba3fa42eddddd8f"
			const spanID = "b3bd5e1c4318c78a"

			// Set B3 headers properly - these are case-sensitive and should use standard names
			req.Header.Set("X-B3-Traceid", traceID)
			req.Header.Set("X-B3-Spanid", spanID)
			req.Header.Set("X-B3-Sampled", "1") // Ensure sampling is enabled

			cm := tracingConfig(test.tracingEnabled)
			cfg, err := config.NewTracingConfigFromConfigMap(cm)
			if err != nil {
				t.Fatal("Failed to parse tracing config", err)
			}

			configStore := activatorconfig.NewStore(logger)
			configStore.OnConfigChanged(cm)
			ctx = configStore.ToContext(ctx)

			exporter := tracetest.NewInMemoryExporter()
			processor := sdktrace.NewSimpleSpanProcessor(exporter)

			// Initialize OpenTelemetry with the exporter and processor
			if err := tracingotel.Init("tracing-handler-test", cfg, logger, processor); err != nil {
				t.Fatal("Failed to initialize tracingotel:", err)
			}

			// Configure B3 propagation to properly extract trace context from headers
			prop := propagation.NewCompositeTextMapPropagator(
				b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader)),
			)
			otel.SetTextMapPropagator(prop)

			t.Cleanup(func() {
				if err := tracingotel.Shutdown(context.Background()); err != nil {
					t.Error("Error shutting down tracingotel:", err)
				}
				exporter.Shutdown(context.Background())
			})

			// Log the tracing configuration for debugging
			t.Logf("Tracing configuration: Backend=%s, Debug=%v, Endpoint=%s",
				cfg.Backend, cfg.Debug, cfg.ZipkinEndpoint)

			handler.ServeHTTP(resp, req.WithContext(ctx))

			spans := exporter.GetSpans()
			t.Logf("Got %d spans after request", len(spans))

			// Log detailed span information for debugging
			for i, span := range spans {
				t.Logf("Span %d: Name=%q, TraceID=%s, ParentSpanID=%s",
					i, span.Name, span.SpanContext.TraceID().String(),
					span.Parent.SpanID().String())
			}

			if test.tracingEnabled {
				if len(spans) != 1 {
					t.Errorf("Got %d spans, expected 1: spans = %v", len(spans), spans)
				} else {
					// Verify the span has the correct name and trace ID
					span := spans[0]
					if span.Name != "server" {
						t.Errorf("Got span name %q, expected %q", span.Name, "server")
					}
					if got := span.SpanContext.TraceID().String(); got != traceID {
						t.Errorf("span.SpanContext.TraceID() = %s, want %s", got, traceID)
					}

					// Check parent span ID
					if got := span.Parent.SpanID().String(); got != spanID {
						t.Errorf("span.Parent.SpanID() = %s, want %s", got, spanID)
					}
				}
			} else if len(spans) != 0 {
				t.Errorf("Got %d spans, expected 0: spans = %v", len(spans), spans)
			}
		})
	}
}

func tracingConfig(enabled bool) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: config.ConfigName,
		},
		Data: map[string]string{
			"backend": "none",
			// Always set sample rate to 1.0 for tests to ensure consistent sampling
			"sample-rate": "1.0",
		},
	}
	if enabled {
		cm.Data["backend"] = "zipkin"
		cm.Data["zipkin-endpoint"] = "http://foo.bar"
		cm.Data["debug"] = "true"
		// Keep sample rate at 1.0 even when enabled
		cm.Data["sample-rate"] = "1.0"
	}
	return cm
}
