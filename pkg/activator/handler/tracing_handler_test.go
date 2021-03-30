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
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/logging"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/tracing"
	"knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	activatorconfig "knative.dev/serving/pkg/activator/config"
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

			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
			handler := NewTracingHandler(baseHandler)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			const traceID = "821e0d50d931235a5ba3fa42eddddd8f"
			req.Header["X-B3-Traceid"] = []string{traceID}
			req.Header["X-B3-Spanid"] = []string{"b3bd5e1c4318c78a"}

			cm := tracingConfig(test.tracingEnabled)
			cfg, err := config.NewTracingConfigFromConfigMap(cm)
			if err != nil {
				t.Fatal("Failed to parse tracing config", err)
			}

			configStore := activatorconfig.NewStore(logging.FromContext(ctx))
			configStore.OnConfigChanged(cm)
			ctx = configStore.ToContext(ctx)

			reporter, co := tracetesting.FakeZipkinExporter()
			oct := tracing.NewOpenCensusTracer(co)
			t.Cleanup(func() {
				reporter.Close()
				oct.Finish()
			})

			if err := oct.ApplyConfig(cfg); err != nil {
				t.Error("Failed to apply tracer config:", err)
			}

			handler.ServeHTTP(resp, req.WithContext(ctx))

			spans := reporter.Flush()

			if test.tracingEnabled {
				if len(spans) != 1 {
					t.Errorf("Got %d spans, expected 1: spans = %v", len(spans), spans)
				}
				if got := spans[0].TraceID.String(); got != traceID {
					t.Errorf("spans[0].TraceID = %s, want %s", got, traceID)
				}
			} else {
				if len(spans) != 0 {
					t.Errorf("Got %d spans, expected 0: spans = %v", len(spans), spans)
				}
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
		},
	}
	if enabled {
		cm.Data["backend"] = "zipkin"
		cm.Data["zipkin-endpoint"] = "foo.bar"
		cm.Data["debug"] = "true"
	}
	return cm
}
