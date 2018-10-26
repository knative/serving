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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/activator/util"
)

type stubActivator struct {
	endpoint  activator.Endpoint
	namespace string
	name      string
}

func newStubActivator(namespace string, name string, server *httptest.Server) activator.Activator {
	url, _ := url.Parse(server.URL)
	host := url.Hostname()
	port, _ := strconv.Atoi(url.Port())

	return &stubActivator{
		endpoint:  activator.Endpoint{FQDN: host, Port: int32(port)},
		namespace: namespace,
		name:      name,
	}
}

func (fa *stubActivator) ActiveEndpoint(namespace, name string) activator.ActivationResult {
	if namespace == fa.namespace && name == fa.name {
		return activator.ActivationResult{
			Status:            http.StatusOK,
			Endpoint:          fa.endpoint,
			ServiceName:       "service-" + fa.name,
			ConfigurationName: "config-" + fa.name,
		}
	}
	return activator.ActivationResult{
		Status: http.StatusNotFound,
		Error:  errors.New("not found"),
	}
}

func (fa *stubActivator) Shutdown() {
}

func TestActivationHandler(t *testing.T) {
	errMsg := func(msg string) string {
		return fmt.Sprintf("Error getting active endpoint: %v\n", msg)
	}

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "everything good!")
		}),
	)
	defer server.Close()

	act := newStubActivator("real-namespace", "real-name", server)

	examples := []struct {
		label         string
		namespace     string
		name          string
		wantBody      string
		wantCode      int
		wantErr       error
		attempts      string
		reporterCalls []reporterCall
	}{
		{
			label:     "active endpoint",
			namespace: "real-namespace",
			name:      "real-name",
			wantBody:  "everything good!",
			wantCode:  http.StatusOK,
			wantErr:   nil,
			attempts:  "123",
			reporterCalls: []reporterCall{
				{
					Op:         "ReportResponseCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
					Attempts:   123,
					Value:      1.0,
				},
				{
					Op:         "ReportResponseTime",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			label:     "active endpoint with missing count header",
			namespace: "real-namespace",
			name:      "real-name",
			wantBody:  "everything good!",
			wantCode:  http.StatusOK,
			wantErr:   nil,
			reporterCalls: []reporterCall{
				{
					Op:         "ReportResponseCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
					Attempts:   1,
					Value:      1.0,
				},
				{
					Op:         "ReportResponseTime",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			label:         "no active endpoint",
			namespace:     "fake-namespace",
			name:          "fake-name",
			wantBody:      errMsg("not found"),
			wantCode:      http.StatusNotFound,
			wantErr:       nil,
			reporterCalls: nil,
		},
		{
			label:     "request error",
			namespace: "real-namespace",
			name:      "real-name",
			wantBody:  "",
			wantCode:  http.StatusBadGateway,
			wantErr:   errors.New("request error"),
			reporterCalls: []reporterCall{
				{
					Op:         "ReportResponseCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusBadGateway,
					Attempts:   1,
					Value:      1.0,
				},
				{
					Op:         "ReportResponseTime",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusBadGateway,
				},
			},
		},
		{
			label:     "invalid number of attempts",
			namespace: "real-namespace",
			name:      "real-name",
			wantBody:  "everything good!",
			wantCode:  http.StatusOK,
			wantErr:   nil,
			attempts:  "hi there",
			reporterCalls: []reporterCall{
				{
					Op:         "ReportResponseCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
					Attempts:   1,
					Value:      1.0,
				},
				{
					Op:         "ReportResponseTime",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
				},
			},
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			rt := util.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if e.wantErr != nil {
					return nil, e.wantErr
				}
				resp, err := http.DefaultTransport.RoundTrip(r)
				if err != nil {
					return resp, err
				}
				if e.attempts != "" {
					resp.Header.Add(activator.RequestCountHTTPHeader, e.attempts)
				}
				return resp, err
			})

			reporter := &fakeReporter{}
			handler := ActivationHandler{
				Activator: act,
				Transport: rt,
				Logger:    TestLogger(t),
				Reporter:  reporter,
			}

			resp := httptest.NewRecorder()

			req := httptest.NewRequest("POST", "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, e.namespace)
			req.Header.Set(activator.RevisionHeaderName, e.name)
			handler.ServeHTTP(resp, req)

			if resp.Code != e.wantCode {
				t.Errorf("Unexpected response status. Want %d, got %d", e.wantCode, resp.Code)
			}

			if resp.Header().Get(activator.RequestCountHTTPHeader) != "" {
				t.Errorf("Expected the %q header to be filtered", activator.RequestCountHTTPHeader)
			}

			gotBody, _ := ioutil.ReadAll(resp.Body)
			if string(gotBody) != e.wantBody {
				t.Errorf("Unexpected response body. Want %q, got %q", e.wantBody, gotBody)
			}

			if diff := cmp.Diff(e.reporterCalls, reporter.calls, ignoreDurationOption); diff != "" {
				t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
			}
		})
	}

}

var ignoreDurationOption = cmpopts.IgnoreFields(reporterCall{}, "Duration")

type reporterCall struct {
	Op         string
	Namespace  string
	Service    string
	Config     string
	Revision   string
	StatusCode int
	Attempts   int
	Value      float64
	Duration   time.Duration
}

type fakeReporter struct {
	calls []reporterCall
}

func (f *fakeReporter) ReportRequest(ns, service, config, rev string, v float64) error {
	return nil
}

func (f *fakeReporter) ReportResponseCount(ns, service, config, rev string, responseCode, numTries int, v float64) error {
	f.calls = append(f.calls, reporterCall{
		Op:         "ReportResponseCount",
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
