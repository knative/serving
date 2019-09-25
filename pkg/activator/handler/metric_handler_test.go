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

package handler

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/network"
)

var ignoreDurationOption = cmpopts.IgnoreFields(reporterCall{}, "Duration")

func TestRequestMetricHandler(t *testing.T) {
	testNamespace := "real-namespace"
	testRevName := "real-name"

	tests := []struct {
		label         string
		baseHandler   http.HandlerFunc
		reporterCalls []reporterCall
		newHeader     map[string]string
		wantCode      int
		wantPanic     bool
	}{
		{
			label: "kube probe request",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			newHeader: map[string]string{"User-Agent": network.KubeProbeUAPrefix},
			wantCode:  http.StatusOK,
		},
		{
			label: "network probe response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			newHeader: map[string]string{network.ProbeHeaderName: "test-service"},
			wantCode:  http.StatusOK,
		},
		{
			label: "normal response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			reporterCalls: []reporterCall{{
				Op:         "ReportResponseTime",
				Namespace:  testNamespace,
				Revision:   testRevName,
				Service:    "service-real-name",
				Config:     "config-real-name",
				StatusCode: http.StatusOK,
			}},
			wantCode: http.StatusOK,
		},
		{
			label: "panic response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				panic(errors.New("handler error"))
			}),
			reporterCalls: []reporterCall{{
				Op:         "ReportResponseTime",
				Namespace:  testNamespace,
				Revision:   testRevName,
				Service:    "service-real-name",
				Config:     "config-real-name",
				StatusCode: http.StatusInternalServerError,
			}},
			wantCode:  http.StatusBadRequest,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer func() {
				cancel()
			}()
			reporter := &fakeReporter{}
			revisionInformer(ctx, revision(testNamespace, testRevName))
			handler := NewMetricHandler(ctx, reporter, test.baseHandler)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
			req.Header.Add(activator.RevisionHeaderNamespace, testNamespace)
			req.Header.Add(activator.RevisionHeaderName, testRevName)
			if test.newHeader != nil && len(test.newHeader) != 0 {
				for k, v := range test.newHeader {
					req.Header.Add(k, v)
				}
			}

			defer func() {
				err := recover()
				if test.wantPanic && err == nil {
					t.Error("Want ServeHTTP to panic, got nothing.")
				}

				if resp.Code != test.wantCode {
					t.Errorf("Response Status = %d,  want: %d", resp.Code, test.wantCode)
				}
				if got, want := reporter.calls, test.reporterCalls; !cmp.Equal(got, want, ignoreDurationOption) {
					t.Errorf("Reporting calls are different (-want, +got) = %s", cmp.Diff(want, got, ignoreDurationOption))
				}
			}()

			handler.ServeHTTP(resp, req)
		})
	}

}
