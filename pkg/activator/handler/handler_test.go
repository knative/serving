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
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/activator/util"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/tracing"
	tracingconfig "github.com/knative/serving/pkg/tracing/config"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	reporterrecorder "github.com/openzipkin/zipkin-go/reporter/recorder"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	wantBody      = "everything good!"
	testNamespace = "real-namespace"
	testRevName   = "real-name"
)

var stubRevisionGetter = func(revID activator.RevisionID) (*v1alpha1.Revision, error) {
	if revID.Namespace != testNamespace {
		return nil, &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Status:  metav1.StatusFailure,
				Code:    http.StatusNotFound,
				Reason:  metav1.StatusReasonNotFound,
				Message: fmt.Sprintf("not found"),
			}}
	}
	revision := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revID.Namespace,
			Name:      revID.Name,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: "config-" + revID.Name,
				serving.ServiceLabelKey:       "service-" + revID.Name,
			},
		},
		Spec: v1alpha1.RevisionSpec{ContainerConcurrency: 1}}
	return revision, nil
}

var stubServiceGetter = func(namespace, name string) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name: "http",
				Port: 8080,
			}},
		}}
	return service, nil
}

func TestActivationHandler(t *testing.T) {
	defer ClearAll()
	goodEndpointsGetter := func(activator.RevisionID) (int32, error) {
		return 1000, nil
	}
	brokenEndpointGetter := func(activator.RevisionID) (int32, error) {
		return 0, errors.New("some error")
	}
	errMsg := func(msg string) string {
		return fmt.Sprintf("Error getting active endpoint: %v\n", msg)
	}

	examples := []struct {
		label           string
		namespace       string
		name            string
		wantBody        string
		wantCode        int
		wantErr         error
		probeErr        error
		probeCode       int
		probeResp       []string
		gpc             int
		endpointsGetter func(activator.RevisionID) (int32, error)
		reporterCalls   []reporterCall
	}{{
		label:           "active endpoint",
		namespace:       testNamespace,
		name:            testRevName,
		wantBody:        "everything good!",
		wantCode:        http.StatusOK,
		wantErr:         nil,
		endpointsGetter: goodEndpointsGetter,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   2, // probe + request
			Value:      1,
		}, {
			Op:         "ReportResponseTime",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
		}},
		gpc: 1,
	}, {
		label:           "slowly active endpoint",
		namespace:       testNamespace,
		name:            testRevName,
		wantBody:        "everything good!",
		wantCode:        http.StatusOK,
		wantErr:         nil,
		probeResp:       []string{activator.Name, queue.Name},
		endpointsGetter: goodEndpointsGetter,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   3, // probe + probe + request
			Value:      1,
		}, {
			Op:         "ReportResponseTime",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
		}},
		gpc: 2,
	}, {
		label:           "active endpoint with missing count header",
		namespace:       testNamespace,
		name:            testRevName,
		wantBody:        "everything good!",
		wantCode:        http.StatusOK,
		wantErr:         nil,
		endpointsGetter: goodEndpointsGetter,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   1,
			Value:      1,
		}, {
			Op:         "ReportResponseTime",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
		}},
	}, {
		label:           "no active endpoint",
		namespace:       "fake-namespace",
		name:            "fake-name",
		wantBody:        errMsg("not found"),
		wantCode:        http.StatusNotFound,
		wantErr:         nil,
		endpointsGetter: goodEndpointsGetter,
		reporterCalls:   nil,
	}, {
		label:           "active endpoint (probe failure)",
		namespace:       testNamespace,
		name:            testRevName,
		probeErr:        errors.New("probe error"),
		wantCode:        http.StatusInternalServerError,
		endpointsGetter: goodEndpointsGetter,
		gpc:             1,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusInternalServerError,
			Attempts:   1,
			Value:      1,
		}, {
			Op:         "ReportResponseTime",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusInternalServerError,
		}},
	}, {
		label:           "active endpoint (probe 500)",
		namespace:       testNamespace,
		name:            testRevName,
		probeCode:       http.StatusServiceUnavailable,
		wantCode:        http.StatusInternalServerError,
		endpointsGetter: goodEndpointsGetter,
		gpc:             1,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusInternalServerError,
			Attempts:   1,
			Value:      1,
		}, {
			Op:         "ReportResponseTime",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusInternalServerError,
		}},
	}, {
		label:           "request error",
		namespace:       testNamespace,
		name:            testRevName,
		wantBody:        "",
		wantCode:        http.StatusBadGateway,
		wantErr:         errors.New("request error"),
		endpointsGetter: goodEndpointsGetter,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusBadGateway,
			Attempts:   1,
			Value:      1,
		}, {
			Op:         "ReportResponseTime",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusBadGateway,
		}},
	}, {
		label:           "invalid number of attempts",
		namespace:       testNamespace,
		name:            testRevName,
		wantBody:        "everything good!",
		wantCode:        http.StatusOK,
		wantErr:         nil,
		endpointsGetter: goodEndpointsGetter,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   1,
			Value:      1,
		}, {
			Op:         "ReportResponseTime",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
		}},
	}, {
		label:           "broken GetEndpoints",
		namespace:       testNamespace,
		name:            testRevName,
		wantBody:        "",
		wantCode:        http.StatusInternalServerError,
		wantErr:         nil,
		endpointsGetter: brokenEndpointGetter,
		reporterCalls:   nil,
	}}

	trGetter := tracerGetter(reporterrecorder.NewReporter())

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			rt := util.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get(network.ProbeHeaderName) != "" {
					if e.probeErr != nil {
						return nil, e.probeErr
					}
					fake := httptest.NewRecorder()
					fake.WriteHeader(e.probeCode)
					probeResp := queue.Name
					if len(e.probeResp) > 0 {
						probeResp = e.probeResp[0]
						e.probeResp = e.probeResp[1:]
					}
					fake.WriteString(probeResp)
					return fake.Result(), nil
				}
				if e.wantErr != nil {
					return nil, e.wantErr
				}

				fake := httptest.NewRecorder()

				fake.WriteHeader(http.StatusOK)
				fake.WriteString(wantBody)
				return fake.Result(), nil
			})

			reporter := &fakeReporter{}
			params := queue.BreakerParams{QueueDepth: 1000, MaxConcurrency: 1000, InitialCapacity: 0}
			throttlerParams := activator.ThrottlerParams{
				BreakerParams: params,
				Logger:        TestLogger(t),
				GetRevision:   stubRevisionGetter,
				GetEndpoints:  e.endpointsGetter,
			}
			handler := ActivationHandler{
				Transport:     rt,
				Logger:        TestLogger(t),
				Reporter:      reporter,
				Throttler:     activator.NewThrottler(throttlerParams),
				TRGetter:      trGetter,
				GetProbeCount: e.gpc,
				GetRevision:   stubRevisionGetter,
				GetService:    stubServiceGetter,
			}

			resp := httptest.NewRecorder()

			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, e.namespace)
			req.Header.Set(activator.RevisionHeaderName, e.name)

			handler.ServeHTTP(resp, req)

			if resp.Code != e.wantCode {
				t.Errorf("Unexpected response status. Want %d, got %d", e.wantCode, resp.Code)
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
		requests      = wantedSuccess + wantedFailure
	)
	respCh := make(chan *httptest.ResponseRecorder, requests)
	// overall max 20 requests in the Breaker
	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}

	namespace := testNamespace
	revName := testRevName

	throttler := getThrottler(breakerParams, t)

	lockerCh := make(chan struct{})
	handler := getHandler(throttler, lockerCh, t)
	sendRequests(requests, namespace, revName, respCh, handler)

	assertResponses(wantedSuccess, wantedFailure, requests, lockerCh, respCh, t)
}

// Make sure if one breaker is overflowed, the requests to other revisions are still served
func TestActivationHandler_OverflowSeveralRevisions(t *testing.T) {
	const (
		wantedSuccess   = 40
		wantedFailure   = 2
		overallRequests = wantedSuccess + wantedFailure
		revisions       = 2
	)

	respCh := make(chan *httptest.ResponseRecorder, overallRequests)
	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	throttler := getThrottler(breakerParams, t)
	lockerCh := make(chan struct{})

	for rev := 0; rev < revisions; rev++ {
		revName := fmt.Sprintf("%s-%d", testRevName, rev)

		handler := getHandler(throttler, lockerCh, t)

		requestCount := overallRequests / revisions
		sendRequests(requestCount, testNamespace, revName, respCh, handler)
	}
	assertResponses(wantedSuccess, wantedFailure, overallRequests, lockerCh, respCh, t)
}

// sendRequests sends `count` concurrent requests via the given handler and writes
// the recorded responses to the `respCh`.
func sendRequests(count int, namespace, revName string, respCh chan *httptest.ResponseRecorder, handler ActivationHandler) {
	for i := 0; i < count; i++ {
		go func() {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, namespace)
			req.Header.Set(activator.RevisionHeaderName, revName)
			handler.ServeHTTP(resp, req)
			respCh <- resp
		}()
	}
}

// getThrottler returns a fully setup Throttler with some sensible defaults for tests.
func getThrottler(breakerParams queue.BreakerParams, t *testing.T) *activator.Throttler {
	endpointsGetter := func(activator.RevisionID) (int32, error) {
		// Since revisions have a concurrency of 1, this will cause the very same capacity
		// as being set initially.
		return breakerParams.InitialCapacity, nil
	}
	throttlerParams := activator.ThrottlerParams{BreakerParams: breakerParams, Logger: TestLogger(t), GetRevision: stubRevisionGetter, GetEndpoints: endpointsGetter}
	throttler := activator.NewThrottler(throttlerParams)
	return throttler
}

func tracerGetter(reporter zipkinreporter.Reporter) tracing.TracerRefGetter {
	reporterFact := func(cfg *tracingconfig.Config) (zipkinreporter.Reporter, error) {
		return reporter, nil
	}
	tc := tracing.TracerCache{
		CreateReporter: reporterFact,
	}
	trGetter := func(ctx context.Context) (*tracing.TracerRef, error) {
		return tc.NewTracerRef(&tracingconfig.Config{}, "testapp", "localhost:1234")
	}
	return trGetter
}

// getHandler returns an already setup ActivationHandler. The roundtripper is controlled
// via the given `lockerCh`.
func getHandler(throttler *activator.Throttler, lockerCh chan struct{}, t *testing.T) ActivationHandler {
	rt := util.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		// Allows only one request at a time until read from.
		lockerCh <- struct{}{}

		fake := httptest.NewRecorder()
		fake.WriteHeader(http.StatusOK)
		fake.WriteString(wantBody)

		return fake.Result(), nil
	})

	trGetter := tracerGetter(reporterrecorder.NewReporter())

	handler := ActivationHandler{
		Transport:   rt,
		Logger:      TestLogger(t),
		Reporter:    &fakeReporter{},
		Throttler:   throttler,
		GetRevision: stubRevisionGetter,
		GetService:  stubServiceGetter,
		TRGetter:    trGetter,
	}
	return handler
}

func assertResponses(wantedSuccess, wantedFailure, overallRequests int, lockerCh chan struct{}, respCh chan *httptest.ResponseRecorder, t *testing.T) {
	t.Helper()

	const channelTimeout = 3 * time.Second
	var (
		successCode = http.StatusOK
		failureCode = http.StatusServiceUnavailable

		succeeded int
		failed    int
	)

	processResponse := func(chan *httptest.ResponseRecorder) {
		select {
		case resp := <-respCh:
			bodyBytes, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read body: %v", err)
			}
			gotBody := strings.TrimSpace(string(bodyBytes))

			switch resp.Code {
			case successCode:
				succeeded++
				if gotBody != wantBody {
					t.Errorf("response body = %q, want: %q", gotBody, wantBody)
				}
			case failureCode:
				failed++
				if gotBody != activator.ErrActivatorOverload.Error() {
					t.Errorf("error message = %q, want: %q", gotBody, activator.ErrActivatorOverload.Error())
				}
			default:
				t.Errorf("http response code = %d, want: %d or %d", resp.Code, successCode, failureCode)
			}
		case <-time.After(channelTimeout):
			t.Fatalf("Timed out waiting for a request to be returned")
		}
	}

	// The failures will arrive first, because we block other requests from being executed
	for i := 0; i < wantedFailure; i++ {
		processResponse(respCh)
	}

	for i := 0; i < wantedSuccess; i++ {
		// All of the success requests are locked via the lockerCh.
		select {
		case <-lockerCh:
			// All good.
		case <-time.After(channelTimeout):
			t.Fatalf("Timed out waiting for a request to reach the RoundTripper")
		}
		processResponse(respCh)
	}

	if wantedFailure != failed {
		t.Errorf("failed request count = %d, want: %d", failed, wantedFailure)
	}
	if succeeded != wantedSuccess {
		t.Errorf("successful request count = %d, want: %d", succeeded, wantedSuccess)
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
