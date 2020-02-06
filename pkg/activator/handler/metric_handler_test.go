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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/types"

	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator"
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
			handler := NewMetricHandler(ctx, reporter, test.baseHandler)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
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

			reqCtx := withRevision(context.Background(), revision(testNamespace, testRevName))
			reqCtx = withRevID(reqCtx, types.NamespacedName{Namespace: testNamespace, Name: testRevName})
			handler.ServeHTTP(resp, req.WithContext(reqCtx))
		})
	}

}

func BenchmarkMetricHandler(b *testing.B) {
	reporter, err := activator.NewStatsReporter("test_pod")
	if err != nil {
		b.Fatalf("Failed to create a reporter: %v", err)
	}
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	reqCtx := withRevision(context.Background(), revision(testNamespace, testRevName))

	handler := &MetricHandler{reporter: reporter, nextHandler: baseHandler}

	resp := httptest.NewRecorder()
	b.Run(fmt.Sprint("sequential"), func(b *testing.B) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil).WithContext(reqCtx)
		for j := 0; j < b.N; j++ {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run(fmt.Sprint("parallel"), func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil).WithContext(reqCtx)
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}
