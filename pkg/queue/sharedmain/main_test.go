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

package sharedmain

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"testing"
	"time"

	// OpenTelemetry imports
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap" // For logger in tracingotel.Init

	"github.com/kelseyhightower/envconfig"
	netheader "knative.dev/networking/pkg/http/header"
	netstats "knative.dev/networking/pkg/http/stats"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
	"knative.dev/serving/pkg/tracingotel"                   // New
	otelconfig "knative.dev/serving/pkg/tracingotel/config" // New
)

func TestQueueTraceSpans(t *testing.T) {
	testcases := []struct {
		name          string
		prober        func() bool
		wantSpans     int
		requestHeader string
		infiniteCC    bool
		probeWillFail bool
		probeTrace    bool
		enableTrace   bool
	}{{
		name:          "proxy trace",
		prober:        func() bool { return true },
		wantSpans:     1,
		requestHeader: "",
		probeWillFail: false,
		probeTrace:    false,
		enableTrace:   true,
	}, {
		name:          "proxy trace, no breaker",
		prober:        func() bool { return true },
		wantSpans:     1,
		requestHeader: "",
		probeWillFail: false,
		probeTrace:    false,
		enableTrace:   true,
		infiniteCC:    true,
	}, {
		name:          "true prober function with probe trace",
		prober:        func() bool { return true },
		wantSpans:     0,
		requestHeader: queue.Name,
		probeWillFail: false,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "unexpected probe header",
		prober:        func() bool { return true },
		wantSpans:     0,
		requestHeader: "test-probe",
		probeWillFail: true,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "nil prober function",
		prober:        nil,
		wantSpans:     0,
		requestHeader: queue.Name,
		probeWillFail: true,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "false prober function",
		prober:        func() bool { return false },
		wantSpans:     0,
		requestHeader: queue.Name,
		probeWillFail: true,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "no traces",
		prober:        func() bool { return true },
		wantSpans:     1,
		requestHeader: queue.Name,
		probeWillFail: false,
		probeTrace:    false,
		enableTrace:   false,
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Create an OTel test exporter
			testExporter := tracetest.NewInMemoryExporter()

			// Setup B3 propagator globally for otelhttp.NewTransport to pick up
			otel.SetTextMapPropagator(b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader | b3.B3SingleHeader)))

			// Configure OpenTelemetry based on test case
			otelTestCfg := &otelconfig.Config{
				Backend:        otelconfig.Zipkin,
				Debug:          true,
				SampleRate:     1.0,
				ZipkinEndpoint: "", // Not strictly needed for testExporter but Init might use it
			}
			if !tc.enableTrace {
				otelTestCfg.Backend = otelconfig.None
			}

			nopLogger := zap.NewNop().Sugar()
			serviceName := "test-queue-proxy-" + tc.name

			err := tracingotel.Init(serviceName, otelTestCfg, nopLogger, sdktrace.NewSimpleSpanProcessor(testExporter))
			if err != nil {
				t.Fatalf("tracingotel.Init() error = %v", err)
			}
			defer func() {
				if err := tracingotel.Shutdown(context.Background()); err != nil {
					t.Logf("tracingotel.Shutdown() error = %v", err)
				}
			}()

			writer := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			tracingIsEnabled := tc.enableTrace

			if !tc.probeTrace {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				defer server.Close()
				serverURL, _ := url.Parse(server.URL)

				proxy := httputil.NewSingleHostReverseProxy(serverURL)
				params := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
				var breaker *queue.Breaker
				if !tc.infiniteCC {
					breaker = queue.NewBreaker(params)
				}
				proxy.Transport = otelhttp.NewTransport(pkgnet.AutoTransport)

				h := queue.ProxyHandler(breaker, netstats.NewRequestStats(time.Now()), tracingIsEnabled, proxy)
				h(writer, req)
			} else {
				h := health.ProbeHandler(tc.prober, tracingIsEnabled)
				req.Header.Set(netheader.ProbeKey, tc.requestHeader)
				h(writer, req)
			}

			roSpans := testExporter.GetSpans()
			if len(roSpans) != tc.wantSpans {
				t.Logf("Spans collected for %s:", tc.name)
				for idx, s := range roSpans {
					t.Logf("Span %d: Name=%s, Kind=%s, Status.Code=%s, Status.Description=%s, Attrs=%v", idx, s.Name, s.SpanKind, s.Status.Code, s.Status.Description, s.Attributes)
				}
				t.Errorf("Got %d OTel spans, expected %d. NOTE: Expected span counts and names might need adjustment.", len(roSpans), tc.wantSpans)
			}

			if tc.probeWillFail && tc.enableTrace && len(roSpans) > 0 {
				relevantSpanIdx := 0 // Assumption: the first span is the one from the probe.
				if relevantSpanIdx < len(roSpans) {
					if roSpans[relevantSpanIdx].Status.Code != codes.Error {
						t.Errorf("Expected span status Error for failed probe, got %s (Description: %s)", roSpans[relevantSpanIdx].Status.Code, roSpans[relevantSpanIdx].Status.Description)
					}
				} else if tc.wantSpans > 0 { // if we expected spans but got none
					t.Errorf("Expected spans for failed probe but got none to check error status.")
				}
			}
		})
	}
}

func TestEnv(t *testing.T) {
	envVars := []struct {
		name      string
		value     any
		fieldName string
	}{{
		"ENABLE_HTTP2_AUTO_DETECTION",
		true,
		"EnableHTTP2AutoDetection",
	}, {
		"CONTAINER_CONCURRENCY",
		10,
		"ContainerConcurrency",
	}, {
		"QUEUE_SERVING_PORT",
		"8080",
		"QueueServingPort ",
	}, {
		"QUEUE_SERVING_TLS_PORT",
		"443",
		"QueueServingTLSPort  ",
	}, {
		"REVISION_TIMEOUT_SECONDS",
		1000,
		"RevisionTimeoutSeconds ",
	}, {
		"USER_PORT",
		"8081",
		"UserPort",
	}, {
		"SERVING_LOGGING_CONFIG",
		"",
		"ServingLoggingConfig ",
	}, {
		"SERVING_LOGGING_LEVEL",
		"info",
		"ServingLoggingLevel",
	}, {
		"SERVING_NAMESPACE",
		"knative-serving",
		"ServingNamespace",
	}, {
		"SERVING_CONFIGURATION",
		"",
		"ServingConfiguration",
	}, {
		"SERVING_REVISION",
		"rev",
		"ServingRevision",
	}, {
		"SERVING_POD",
		"pod",
		"ServingPod",
	}, {
		"SERVING_POD_IP",
		"1.1.1.1",
		"ServingPodIP",
	}}

	for _, v := range envVars {
		var str string
		switch tp := v.value.(type) {
		case int:
			str = fmt.Sprintf("%d", v.value)
		case bool:
			str = fmt.Sprintf("%t", v.value)
		case string:
			str = v.value.(string)
		default:
			t.Fatalf("Unexpected type: %v", tp)
		}

		t.Setenv(v.name, str)
	}
	var env config
	if err := envconfig.Process("", &env); err != nil {
		t.Fatal("Got unexpected error processing env:", err)
	}

	for _, envVar := range envVars {
		value := getFieldValue(&env, envVar.fieldName)
		switch value.Kind() {
		case reflect.Bool:
			if value.Bool() != envVar.value.(bool) {
				t.Fatalf("Env value for %s is %v, want %v", envVar.name, value.Bool(), envVar.value.(bool))
			}
		case reflect.String:
			if value.String() != envVar.value.(string) {
				t.Fatalf("Env value for %s is %v, want %v", envVar.name, value.String(), envVar.value.(string))
			}
		case reflect.Int:
			if int(value.Int()) != envVar.value.(int) {
				t.Fatalf("Env value for %s is %v, want %v", envVar.name, value.Int(), envVar.value.(int))
			}
		}
	}
}

// getFieldValue extracts the value of a field in env config
func getFieldValue(cfg *config, fieldName string) reflect.Value {
	rVal := reflect.ValueOf(cfg)
	f := reflect.Indirect(rVal).FieldByName(fieldName)
	return f
}
