/*
Copyright 2020 The Knative Authors

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

package health

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/serving/pkg/queue"
)

func TestProbeHandler(t *testing.T) {
	testcases := []struct {
		name          string
		prober        func() bool
		notAProbe     bool
		wantCode      int
		wantBody      string
		requestHeader string
	}{{
		name:          "unexpected probe header",
		prober:        func() bool { return true },
		wantCode:      http.StatusBadRequest,
		wantBody:      badProbeTemplate + "test-probe",
		requestHeader: "test-probe",
	}, {
		name:          "true probe function",
		prober:        func() bool { return true },
		wantCode:      http.StatusOK,
		wantBody:      queue.Name,
		requestHeader: queue.Name,
	}, {
		name:          "nil probe function",
		prober:        nil,
		wantCode:      http.StatusInternalServerError,
		wantBody:      "no probe",
		requestHeader: queue.Name,
	}, {
		name:          "false probe function",
		prober:        func() bool { return false },
		wantCode:      http.StatusServiceUnavailable,
		requestHeader: queue.Name,
	}, {
		name:      "must be called with a probe header",
		notAProbe: true,
		wantCode:  http.StatusBadRequest,
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
			tracer := tp.Tracer("test")

			writer := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

			if !tc.notAProbe {
				req.Header.Set(netheader.ProbeKey, tc.requestHeader)
			}

			h := ProbeHandler(tracer, tc.prober)
			h(writer, req)

			if got, want := writer.Code, tc.wantCode; got != want {
				t.Errorf("probe status = %v, want: %v", got, want)
			}
			if !tc.notAProbe {
				if got, want := strings.TrimSpace(writer.Body.String()), tc.wantBody; got != want {
					// \r\n might be inserted, etc.
					t.Errorf("probe body = %q, want: %q, diff: %s", got, want, cmp.Diff(got, want))
				}
			}
		})
	}
}
