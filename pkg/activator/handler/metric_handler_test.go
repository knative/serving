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
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"net/http"
	"net/http/httptest"
	"testing"

	. "knative.dev/pkg/logging/testing"
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
	}{
		{
			label: "kube probe request",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			newHeader:     map[string]string{"User-Agent": network.KubeProbeUAPrefix},
		},
		{
			label: "network probe response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			newHeader:     map[string]string{network.ProbeHeaderName: "test-service"},
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
		},
		{
			label: "panic response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
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
		},
	}

	reporter := &fakeReporter{}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			defer func() {
				recover()
			}()
			handler := NewMetricHandler(revisionLister(revision(testNamespace, testRevName)), reporter,
				TestLogger(t), test.baseHandler)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
			req.Header.Add(activator.RevisionHeaderNamespace, testNamespace)
			req.Header.Add(activator.RevisionHeaderName, testRevName)
			if test.newHeader != nil && len(test.newHeader) != 0 {
				for k, v := range test.newHeader {
					req.Header.Add(k, v)
				}
			}

			handler.ServeHTTP(resp, req)

			if diff := cmp.Diff(test.reporterCalls, reporter.calls, ignoreDurationOption); diff != "" {
				t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
			}
		})
	}

}
