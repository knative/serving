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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	netstats "knative.dev/networking/pkg/http/stats"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/serving/pkg/queue"
)

func TestQueueTraceSpans(t *testing.T) {
	testcases := []struct {
		name          string
		wantSpans     int
		requestHeader string
		infiniteCC    bool
	}{{
		name:          "proxy trace",
		wantSpans:     3,
		requestHeader: "",
	}, {
		name:          "proxy trace, no breaker",
		wantSpans:     2,
		requestHeader: "",
		infiniteCC:    true,
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
			tracer := tp.Tracer("test")

			reader := metric.NewManualReader()
			mp := metric.NewMeterProvider(metric.WithReader(reader))

			writer := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

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

			proxy.Transport = otelhttp.NewTransport(
				pkgnet.AutoTransport,
				otelhttp.WithMeterProvider(mp),
				otelhttp.WithTracerProvider(tp),
				otelhttp.WithSpanNameFormatter(func(op string, r *http.Request) string {
					return r.URL.Path
				}),
			)

			h := queue.ProxyHandler(tracer, breaker, netstats.NewRequestStats(time.Now()), proxy)
			h(writer, req)

			gotSpans := exporter.GetSpans()
			if len(gotSpans) != tc.wantSpans {
				t.Errorf("Got %d spans, expected %d", len(gotSpans), tc.wantSpans)
			}
			spanNames := []string{"/", "kn.queueproxy.proxy"}

			// We want to add `queueWait` span only if there is possible queueing
			// and if the tests actually expects tracing.
			if !tc.infiniteCC && tc.wantSpans > 1 {
				spanNames = append([]string{"kn.queueproxy.wait"}, spanNames...)
			}
			gs := []string{}
			for i := range gotSpans {
				gs = append(gs, gotSpans[i].Name)
			}
			t.Log(spanNames)
			t.Log(gs)
			for i, spanName := range spanNames[:tc.wantSpans] {
				if gotSpans[i].Name != spanName {
					t.Errorf("Span[%d].Name = %q, want: %q", i, gotSpans[i].Name, spanName)
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

func TestApplyOptions(t *testing.T) {
	t.Run("option sets Transport on proxyHandler", func(t *testing.T) {
		d := &Defaults{}
		proxyHandler := &httputil.ReverseProxy{}
		customTransport := &http.Transport{}

		opt := func(d *Defaults) {
			d.Transport = customTransport
		}

		applyOptions(d, proxyHandler, opt)

		if proxyHandler.Transport != customTransport {
			t.Errorf("proxyHandler.Transport = %v, want %v", proxyHandler.Transport, customTransport)
		}
	})

	t.Run("option receives non-nil ProxyHandler", func(t *testing.T) {
		d := &Defaults{}
		proxyHandler := &httputil.ReverseProxy{}
		var receivedHandler http.Handler

		opt := func(d *Defaults) {
			receivedHandler = d.ProxyHandler
		}

		applyOptions(d, proxyHandler, opt)

		if receivedHandler == nil {
			t.Error("option function received nil ProxyHandler, want non-nil")
		}
		if receivedHandler != proxyHandler {
			t.Errorf("option function received %v, want %v", receivedHandler, proxyHandler)
		}
	})

	t.Run("multiple options with handler and transport changes", func(t *testing.T) {
		d := &Defaults{}
		proxyHandler := &httputil.ReverseProxy{}
		customTransport := &http.Transport{}
		wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		opt1 := func(d *Defaults) {
			d.ProxyHandler = wrappedHandler
		}

		opt2 := func(d *Defaults) {
			d.Transport = customTransport
		}

		applyOptions(d, proxyHandler, opt1, opt2)

		if proxyHandler.Transport != customTransport {
			t.Errorf("proxyHandler.Transport = %v, want %v", proxyHandler.Transport, customTransport)
		}
	})
}
