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
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"

	"go.opencensus.io/plugin/ochttp"

	"github.com/kelseyhightower/envconfig"
	netheader "knative.dev/networking/pkg/http/header"
	netstats "knative.dev/networking/pkg/http/stats"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/pkg/tracing/propagation/tracecontextb3"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
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
		wantSpans:     3,
		requestHeader: "",
		probeWillFail: false,
		probeTrace:    false,
		enableTrace:   true,
	}, {
		name:          "proxy trace, no breaker",
		prober:        func() bool { return true },
		wantSpans:     2,
		requestHeader: "",
		probeWillFail: false,
		probeTrace:    false,
		enableTrace:   true,
		infiniteCC:    true,
	}, {
		name:          "true prober function with probe trace",
		prober:        func() bool { return true },
		wantSpans:     1,
		requestHeader: queue.Name,
		probeWillFail: false,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "unexpected probe header",
		prober:        func() bool { return true },
		wantSpans:     1,
		requestHeader: "test-probe",
		probeWillFail: true,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "nil prober function",
		prober:        nil,
		wantSpans:     1,
		requestHeader: queue.Name,
		probeWillFail: true,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "false prober function",
		prober:        func() bool { return false },
		wantSpans:     1,
		requestHeader: queue.Name,
		probeWillFail: true,
		probeTrace:    true,
		enableTrace:   true,
	}, {
		name:          "no traces",
		prober:        func() bool { return true },
		wantSpans:     0,
		requestHeader: queue.Name,
		probeWillFail: false,
		probeTrace:    false,
		enableTrace:   false,
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Create tracer with reporter recorder
			reporter, co := tracetesting.FakeZipkinExporter()
			defer reporter.Close()
			oct := tracing.NewOpenCensusTracer(co)
			defer oct.Shutdown(context.Background())

			cfg := tracingconfig.Config{
				Backend: tracingconfig.Zipkin,
				Debug:   true,
			}
			if !tc.enableTrace {
				cfg.Backend = tracingconfig.None
			}
			if err := oct.ApplyConfig(&cfg); err != nil {
				t.Error("Failed to apply tracer config:", err)
			}

			writer := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

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
				proxy.Transport = &ochttp.Transport{
					Base:        pkgnet.AutoTransport,
					Propagation: tracecontextb3.TraceContextB3Egress,
				}

				h := queue.ProxyHandler(breaker, netstats.NewRequestStats(time.Now()), true /*tracingEnabled*/, proxy)
				h(writer, req)
			} else {
				h := health.ProbeHandler(tc.prober, true /*tracingEnabled*/)
				req.Header.Set(netheader.ProbeKey, tc.requestHeader)
				h(writer, req)
			}

			gotSpans := reporter.Flush()
			if len(gotSpans) != tc.wantSpans {
				t.Errorf("Got %d spans, expected %d", len(gotSpans), tc.wantSpans)
			}
			spanNames := []string{"probe", "/", "queue_proxy"}
			if !tc.probeTrace {
				spanNames = spanNames[1:]
			}
			// We want to add `queueWait` span only if there is possible queueing
			// and if the tests actually expects tracing.
			if !tc.infiniteCC && tc.wantSpans > 1 {
				spanNames = append([]string{"queue_wait"}, spanNames...)
			}
			gs := []string{}
			for i := 0; i < len(gotSpans); i++ {
				gs = append(gs, gotSpans[i].Name)
			}
			t.Log(spanNames)
			t.Log(gs)
			for i, spanName := range spanNames[:tc.wantSpans] {
				if gotSpans[i].Name != spanName {
					t.Errorf("Span[%d].Name = %q, want: %q", i, gotSpans[i].Name, spanName)
				}
				if tc.probeWillFail {
					if len(gotSpans[i].Annotations) == 0 {
						t.Error("Expected error as value for failed span Annotation, got empty Annotation")
					} else if gotSpans[i].Annotations[0].Value != "error" {
						t.Errorf("Expected error as value for failed span Annotation, got %q", gotSpans[i].Annotations[0].Value)
					}
				}
			}
		})
	}
}

type UnsupportedEnvConfig struct {
	EnableHTTP2AutoDetection bool `split_words:"true"` // optional
}

func TestEnv(t *testing.T) {
	envVars := []struct {
		name  string
		value string
	}{{
		"ENABLE_HTT_P2_AUTO_DETECTION",
		"true",
	}, {
		"ENABLE_HTTP_AUTO_DETECTION",
		"true",
	}, {
		"CONTAINER_CONCURRENCY",
		"10",
	}, {
		"QUEUE_SERVING_PORT",
		"8080",
	}, {
		"QUEUE_SERVING_TLS_PORT",
		"443",
	}, {
		"REVISION_TIMEOUT_SECONDS",
		"1000",
	}, {
		"USER_PORT",
		"8081",
	}, {
		"SERVING_LOGGING_CONFIG",
		"",
	}, {
		"SERVING_LOGGING_LEVEL",
		"info",
	}, {
		"SERVING_NAMESPACE",
		"knative-serving",
	}, {
		"SERVING_CONFIGURATION",
		"",
	}, {
		"SERVING_REVISION",
		"rev",
	}, {
		"SERVING_POD",
		"pod",
	}, {
		"SERVING_POD_IP",
		"1.1.1.1",
	}}

	for _, v := range envVars {
		t.Setenv(v.name, v.value)
	}
	var env config
	if err := envconfig.Process("", &env); err != nil {
		t.Fatal("Got unexpected error processing env:", err)
	}
	if !env.EnableHTTPAutoDetection {
		t.Fatal("Flag ENABLE_HTTP_AUTO_DETECTION should be set to true")
	}
	var uEnv UnsupportedEnvConfig
	if err := envconfig.Process("", &uEnv); err != nil {
		t.Fatal("Got unexpected error processing env:", err)
	}
	// Splitting should have been ENABLE_HTTP2_AUTO_DETECTION
	// Keeping this here as a warning
	if !uEnv.EnableHTTP2AutoDetection {
		t.Fatal("Flag ENABLE_HTT_P2_AUTO_DETECTION should be set to true")
	}
}
