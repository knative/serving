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
	"go.uber.org/atomic"

	"knative.dev/pkg/network"
	"knative.dev/serving/pkg/queue"
)

func TestProbeHandler(t *testing.T) {
	var passed atomic.Int32
	incHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		passed.Inc()
	})
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
		name:      "not a probe, verify chaining",
		notAProbe: true,
		wantCode:  http.StatusOK,
	}}

	healthState := &State{}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

			if !tc.notAProbe {
				req.Header.Set(network.ProbeHeaderName, tc.requestHeader)
			}

			h := ProbeHandler(healthState, tc.prober, true /* isAggressive*/, true /*tracingEnabled*/, incHandler)
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
