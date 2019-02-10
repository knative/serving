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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/activator/util"
	v1alpha12 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/queue"
)

const (
	wantBody    = "everything good!"
	successCode = http.StatusOK
	failureCode = http.StatusServiceUnavailable
)

var stubRevisionGetter = func(activator.RevisionID) (*v1alpha12.Revision, error) {
	revision := &v1alpha12.Revision{Spec: v1alpha12.RevisionSpec{ContainerConcurrency: 1}}
	return revision, nil
}

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
	goodEndpointsGetter := func(activator.RevisionID) (int32, error) {
		return 1000, nil
	}
	brokenEndpointGetter := func(activator.RevisionID) (int32, error) {
		return 0, errors.New("some error")
	}
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
		label           string
		namespace       string
		name            string
		wantBody        string
		wantCode        int
		wantErr         error
		attempts        string
		endpointsGetter func(activator.RevisionID) (int32, error)
		reporterCalls   []reporterCall
	}{
		{
			label:           "active endpoint",
			namespace:       "real-namespace",
			name:            "real-name",
			wantBody:        "everything good!",
			wantCode:        http.StatusOK,
			wantErr:         nil,
			attempts:        "123",
			endpointsGetter: goodEndpointsGetter,
			reporterCalls: []reporterCall{
				{
					Op:         "ReportRequestCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
					Attempts:   123,
					Value:      1,
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
			label:           "active endpoint with missing count header",
			namespace:       "real-namespace",
			name:            "real-name",
			wantBody:        "everything good!",
			wantCode:        http.StatusOK,
			wantErr:         nil,
			endpointsGetter: goodEndpointsGetter,
			reporterCalls: []reporterCall{
				{
					Op:         "ReportRequestCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
					Attempts:   1,
					Value:      1,
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
			label:           "no active endpoint",
			namespace:       "fake-namespace",
			name:            "fake-name",
			wantBody:        errMsg("not found"),
			wantCode:        http.StatusNotFound,
			wantErr:         nil,
			endpointsGetter: goodEndpointsGetter,
			reporterCalls:   nil,
		},
		{
			label:           "request error",
			namespace:       "real-namespace",
			name:            "real-name",
			wantBody:        "",
			wantCode:        http.StatusBadGateway,
			wantErr:         errors.New("request error"),
			endpointsGetter: goodEndpointsGetter,
			reporterCalls: []reporterCall{
				{
					Op:         "ReportRequestCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusBadGateway,
					Attempts:   1,
					Value:      1,
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
			label:           "invalid number of attempts",
			namespace:       "real-namespace",
			name:            "real-name",
			wantBody:        "everything good!",
			wantCode:        http.StatusOK,
			wantErr:         nil,
			attempts:        "hi there",
			endpointsGetter: goodEndpointsGetter,
			reporterCalls: []reporterCall{
				{
					Op:         "ReportRequestCount",
					Namespace:  "real-namespace",
					Revision:   "real-name",
					Service:    "service-real-name",
					Config:     "config-real-name",
					StatusCode: http.StatusOK,
					Attempts:   1,
					Value:      1,
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
			label:           "broken GetEndpoints",
			namespace:       "real-namespace",
			name:            "real-name",
			wantBody:        "",
			wantCode:        http.StatusInternalServerError,
			wantErr:         nil,
			attempts:        "hi there",
			endpointsGetter: brokenEndpointGetter,
			reporterCalls:   nil,
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
			params := queue.BreakerParams{QueueDepth: 1000, MaxConcurrency: 1000, InitialCapacity: 0}
			throttlerParams := activator.ThrottlerParams{BreakerParams: params, Logger: TestLogger(t), GetRevision: stubRevisionGetter, GetEndpoints: e.endpointsGetter}
			handler := ActivationHandler{
				Activator: act,
				Transport: rt,
				Logger:    TestLogger(t),
				Reporter:  reporter,
				Throttler: activator.NewThrottler(throttlerParams),
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
				t.Errorf("Unexpected response body. Response body %q, want %q", gotBody, e.wantBody)
			}

			if diff := cmp.Diff(e.reporterCalls, reporter.calls, ignoreDurationOption); diff != "" {
				t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
			}
		})
	}

}

// Make sure we return http internal server error when the Breaker is overflowed
func TestActivationHandler_Overflow(t *testing.T) {
	const (
		wantedSuccess = 20
		wantedFailure = 1
		requests      = 21
	)
	respCh := make(chan *httptest.ResponseRecorder, requests)
	// overall max 20 requests in the Breaker
	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}

	conf := Config{
		namespace:   "real-namespace",
		revName:     "real-name",
		serviceName: "real-name-service",
		wantBody:    wantBody,
		requests:    requests,
		respCh:      respCh,
	}
	throttler := getThrottler(breakerParams, t)
	run(conf, throttler, t)

	assertResponses(wantedSuccess, wantedFailure, requests, respCh, t)
}

// Make sure if one breaker is overflowed, the requests to other revisions are still served
func TestActivationHandler_OverflowSeveralRevisions(t *testing.T) {
	const (
		wantedSuccess   = 40
		wantedFailure   = 2
		overallRequests = 42
		revisions       = 2
	)

	respCh := make(chan *httptest.ResponseRecorder, overallRequests)

	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}

	throttler := getThrottler(breakerParams, t)

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, wantBody)
		}),
	)

	for rev := 0; rev < revisions; rev++ {
		config := Config{
			namespace:   fmt.Sprintf("real-namespace-%d", rev),
			revName:     "real-name",
			serviceName: "real-name-service",
			wantBody:    wantBody,
			requests:    21,
			respCh:      respCh,
			revisions:   revisions,
		}
		act := newStubActivator(config.namespace, config.revName, server)
		handler := getHandler(throttler, t, act)
		sendRequests(config, handler)
	}
	assertResponses(wantedSuccess, wantedFailure, overallRequests, respCh, t)
}

type Config struct {
	namespace     string
	revName       string
	serviceName   string
	requests      int
	respCh        chan *httptest.ResponseRecorder
	wantBody      string
	breakerParams queue.BreakerParams
	revisions     int
}

func sendRequests(config Config, handler ActivationHandler) {
	for i := 0; i < config.requests; i++ {
		go func() {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, config.namespace)
			req.Header.Set(activator.RevisionHeaderName, config.revName)
			handler.ServeHTTP(resp, req)
			config.respCh <- resp
		}()
	}
}

func getThrottler(breakerParams queue.BreakerParams, t *testing.T) *activator.Throttler {
	endpointsGetter := func(activator.RevisionID) (int32, error) {
		return breakerParams.InitialCapacity, nil
	}
	throttlerParams := activator.ThrottlerParams{BreakerParams: breakerParams, Logger: TestLogger(t), GetRevision: stubRevisionGetter, GetEndpoints: endpointsGetter}
	throttler := activator.NewThrottler(throttlerParams)
	return throttler
}

func run(conf Config, throttler *activator.Throttler, t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, conf.wantBody)
		}),
	)
	act := newStubActivator(conf.namespace, conf.revName, server)
	handler := getHandler(throttler, t, act)
	sendRequests(conf, handler)
}

func getHandler(throttler *activator.Throttler, t *testing.T, act activator.Activator) ActivationHandler {
	rt := util.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			return resp, err
		}
		// Simulate a real request round trip to avoid race condition.
		// If sending N requests that equals to the number of slots, we need to make sure that
		// N+1 won't get a slot that is freed up by one of the "speedy" responses.
		time.Sleep(50 * time.Millisecond)
		return resp, nil
	})
	handler := ActivationHandler{
		Activator: act,
		Transport: rt,
		Logger:    TestLogger(t),
		Reporter:  &fakeReporter{},
		Throttler: throttler,
	}
	return handler
}

func assertResponses(wantedSuccess, wantedFailure, overallRequests int, respCh chan *httptest.ResponseRecorder, t *testing.T) {
	t.Helper()
	var succeeded int
	var failed int
	wantedOverflowMessage := activator.ErrActivatorOverload.Error() + "\n"
	for i := 0; i < overallRequests; i++ {
		resp := <-respCh
		switch resp.Code {
		case successCode:
			gotBody, _ := ioutil.ReadAll(resp.Body)
			if string(gotBody) != wantBody {
				t.Errorf("Unexpected response body. Want %q, got %q", wantBody, gotBody)
			}
			succeeded++
		case failureCode:
			if wantedOverflowMessage != resp.Body.String() {
				t.Errorf("Unexpected error message. Want %s, got %s", wantedOverflowMessage, resp.Body)
			}
			failed++
		default:
			t.Errorf("Unknown http response code was received. Want either %d|%d, got %d", successCode, failureCode, resp.Code)
		}
	}
	if wantedFailure != failed {
		t.Errorf("Unexpected number of failed requests. Want %d, got %d", wantedFailure, failed)
	}
	if succeeded != wantedSuccess {
		t.Errorf("Unexpected number of succeeded requests. Want %d, got %d", wantedSuccess, succeeded)
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
