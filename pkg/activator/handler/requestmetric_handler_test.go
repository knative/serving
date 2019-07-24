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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	testing2 "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/activator"
)

var ignoreDurationOption = cmpopts.IgnoreFields(reporterCall{}, "Duration")

type reporterCall struct {
	Op         string
	Namespace  string
	Service    string
	Config     string
	Revision   string
	StatusCode int
	Attempts   int
	Value      int64
	Duration   time.Duration
}

type fakeReporter struct {
	calls []reporterCall
	mux   sync.Mutex
}

func (f *fakeReporter) ReportRequestCount(ns, service, config, rev string, responseCode, numTries int, v int64) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.calls = append(f.calls, reporterCall{
		Op:         "ReportRequestCount",
		Namespace:  ns,
		Service:    service,
		Config:     config,
		Revision:   rev,
		StatusCode: responseCode,
		Attempts:   numTries,
		Value:      v,
	})

	return nil
}

func (f *fakeReporter) ReportResponseTime(ns, service, config, rev string, responseCode int, d time.Duration) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.calls = append(f.calls, reporterCall{
		Op:         "ReportResponseTime",
		Namespace:  ns,
		Service:    service,
		Config:     config,
		Revision:   rev,
		StatusCode: responseCode,
		Duration:   d,
	})

	return nil
}

func TestRequestMetricHandler(t *testing.T) {
	testNamespace := "real-namespace"
	testRevName := "real-name"

	tests := []struct {
		label         string
		baseHandler   http.HandlerFunc
		reporterCalls []reporterCall
	}{
		{
			label: "normal response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(activator.ProxyAttempts, "2")
				w.WriteHeader(http.StatusOK)
			}),
			reporterCalls: []reporterCall{{
				Op:         "ReportRequestCount",
				Namespace:  testNamespace,
				Revision:   testRevName,
				Service:    "service-real-name",
				Config:     "config-real-name",
				StatusCode: http.StatusOK,
				Attempts:   2,
				Value:      1,
			}, {
				Op:         "ReportResponseTime",
				Namespace:  testNamespace,
				Revision:   testRevName,
				Service:    "service-real-name",
				Config:     "config-real-name",
				StatusCode: http.StatusOK,
			}},
		},
		{
			label: "panic response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				panic(errors.New("handler error"))
			}),
			reporterCalls: []reporterCall{{
				Op:         "ReportRequestCount",
				Namespace:  testNamespace,
				Revision:   testRevName,
				Service:    "service-real-name",
				Config:     "config-real-name",
				StatusCode: http.StatusInternalServerError,
				Attempts:   2,
				Value:      1,
			}, {
				Op:         "ReportResponseTime",
				Namespace:  testNamespace,
				Revision:   testRevName,
				Service:    "service-real-name",
				Config:     "config-real-name",
				StatusCode: http.StatusInternalServerError,
			}},
		},
	}

	reporter := &fakeReporter{}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			defer func() {
				recover()
			}()
			handler := NewRequestMetricHandler(revisionLister(revision(testNamespace, testRevName)), reporter,
				testing2.TestLogger(t), test.baseHandler)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
			req.Header.Add(activator.RevisionHeaderNamespace, testNamespace)
			req.Header.Add(activator.RevisionHeaderName, testRevName)

			handler.ServeHTTP(resp, req)

			if diff := cmp.Diff(test.reporterCalls, reporter.calls, ignoreDurationOption); diff != "" {
				t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
			}
		})
	}

}
