/*
Copyright 2018 The Knative Authors
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
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/google/go-cmp/cmp"

	"github.com/knative/serving/pkg/activator"

)

func TestReporterHandlerResponseReceived(t *testing.T) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add(activator.ResponseCountHTTPHeader, "1234")
		w.WriteHeader(http.StatusTeapot)
	})

	reporter := &fakeReporter{}
	handler := ReportingHTTPHandler{NextHandler: baseHandler, Reporter: reporter}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://example.com", nil)

	req.Header.Add(activator.RevisionHeaderNamespace, "revision-namespace")
	req.Header.Add(activator.RevisionHeaderName, "revision-name")
	req.Header.Add(activator.ConfigurationHeader, "configuration-name")

	handler.ServeHTTP(resp, req)

	want := []call{
		{
			Op:         "ReportResponseCount",
			Namespace:  "revision-namespace",
			Revision:   "revision-name",
			Config:     "configuration-name",
			StatusCode: http.StatusTeapot,
			Attempts:   1234,
			Version:    1.0,
		},
		{
			Op:         "ReportResponseTime",
			Namespace:  "revision-namespace",
			Revision:   "revision-name",
			Config:     "configuration-name",
			StatusCode: http.StatusTeapot,
		},
	}

	got := reporter.calls

	if diff := cmp.Diff(want, got, ignoreDurationOption); diff != "" {
		t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
	}

	if got[1].Duration == 0 {
		t.Errorf("Expected a ReportResponseTime duration > 0")
	}
}

func TestReporterHandlerCountHeaderMissing(t *testing.T) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	reporter := &fakeReporter{}
	handler := ReportingHTTPHandler{NextHandler: baseHandler, Reporter: reporter}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://example.com", nil)

	req.Header.Add(activator.RevisionHeaderNamespace, "revision-namespace")
	req.Header.Add(activator.RevisionHeaderName, "revision-name")
	req.Header.Add(activator.ConfigurationHeader, "configuration-name")

	handler.ServeHTTP(resp, req)

	want := []call{
		{
			Op:         "ReportResponseTime",
			Namespace:  "revision-namespace",
			Revision:   "revision-name",
			Config:     "configuration-name",
			StatusCode: http.StatusTeapot,
		},
	}

	got := reporter.calls

	if diff := cmp.Diff(want, got, ignoreDurationOption); diff != "" {
		t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
	}
}

var ignoreDurationOption = cmpopts.IgnoreFields(call{}, "Duration")

type call struct {
	Op         string
	Namespace  string
	Config     string
	Revision   string
	StatusCode int
	Attempts   int
	Version    float64
	Duration   time.Duration
}

type fakeReporter struct {
	calls []call
}

func (f *fakeReporter) ReportRequest(ns, config, rev, servingState string, v float64) error {
	return nil
}

func (f *fakeReporter) ReportResponseCount(ns, config, rev string, responseCode, numTries int, v float64) error {
	f.calls = append(f.calls, call{
		Op:         "ReportResponseCount",
		Namespace:  ns,
		Config:     config,
		Revision:   rev,
		StatusCode: responseCode,
		Attempts:   numTries,
		Version:    v,
	})

	return nil
}

func (f *fakeReporter) ReportResponseTime(ns, config, rev string, responseCode int, d time.Duration) error {
	f.calls = append(f.calls, call{
		Op:         "ReportResponseTime",
		Namespace:  ns,
		Config:     config,
		Revision:   rev,
		StatusCode: responseCode,
		Duration:   d,
	})

	return nil
}
