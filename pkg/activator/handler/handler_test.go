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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opencensus.io/plugin/ochttp"
	"knative.dev/pkg/ptr"

	activatorconfig "knative.dev/serving/pkg/activator/config"

	"knative.dev/pkg/test/helpers"

	"github.com/google/go-cmp/cmp"

	. "knative.dev/pkg/logging/testing"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/activator"
	activatortest "knative.dev/serving/pkg/activator/testing"
	nv1a1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	servingfake "knative.dev/serving/pkg/client/clientset/versioned/fake"
	servinginformers "knative.dev/serving/pkg/client/informers/externalversions"
	netlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corev1listers "k8s.io/client-go/listers/core/v1"
	. "knative.dev/pkg/configmap/testing"
)

const (
	wantBody         = "everything good!"
	testNamespace    = "real-namespace"
	testRevName      = "real-name"
	testRevNameOther = "other-name"
)

func TestActivationHandler(t *testing.T) {
	defer ClearAll()

	tests := []struct {
		label             string
		namespace         string
		name              string
		wantBody          string
		wantCode          int
		wantErr           error
		probeErr          error
		probeCode         int
		probeResp         []string
		probeTimeout      time.Duration
		endpointsInformer corev1informers.EndpointsInformer
		sksLister         netlisters.ServerlessServiceLister
		svcLister         corev1listers.ServiceLister
		reporterCalls     []reporterCall
	}{{
		label:             "active endpoint",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          "everything good!",
		wantCode:          http.StatusOK,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   2, // probe + request
			Value:      1,
		}},
		probeTimeout: 100 * time.Millisecond,
	}, {
		label:             "slowly active endpoint",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          "everything good!",
		wantCode:          http.StatusOK,
		wantErr:           nil,
		probeResp:         []string{activator.Name, queue.Name},
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   3, // probe + probe + request
			Value:      1,
		}},
		probeTimeout: 201 * time.Millisecond,
	}, {
		label:             "active endpoint with missing count header",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          "everything good!",
		wantCode:          http.StatusOK,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   2, // one probe call, one proxy call.
			Value:      1,
		}},
	}, {
		label:             "no active endpoint",
		namespace:         "fake-namespace",
		name:              "fake-name",
		wantBody:          errMsg("revision.serving.knative.dev \"fake-name\" not found"),
		wantCode:          http.StatusNotFound,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		reporterCalls:     nil,
	}, {
		label:             "active endpoint (probe failure)",
		namespace:         testNamespace,
		name:              testRevName,
		probeErr:          errors.New("probe error"),
		wantCode:          http.StatusInternalServerError,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		probeTimeout:      1 * time.Millisecond,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusInternalServerError,
			Attempts:   2, // On failed probe we'll always try twice.
			Value:      1,
		}},
	}, {
		label:             "active endpoint (probe 500)",
		namespace:         testNamespace,
		name:              testRevName,
		probeCode:         http.StatusServiceUnavailable,
		wantCode:          http.StatusInternalServerError,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		probeTimeout:      10 * time.Millisecond,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusInternalServerError,
			Attempts:   2,
			Value:      1,
		}},
	}, {
		label:             "request error",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          "request error\n",
		wantCode:          http.StatusBadGateway,
		wantErr:           errors.New("request error"),
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusBadGateway,
			Attempts:   2, // probe + actual request.
			Value:      1,
		}},
	}, {
		label:             "broken get SKS",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          errMsg("serverlessservice.networking.internal.knative.dev \"real-name\" not found"),
		wantCode:          http.StatusNotFound,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		sksLister:         sksLister(sks("bogus-namespace", testRevName)),
		reporterCalls:     nil,
	}, {
		label:             "k8s svc incorrectly spec'd",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          errMsg("revision needs external HTTP port"),
		wantCode:          http.StatusInternalServerError,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000)),
		svcLister:         serviceLister(service(testNamespace, testRevName, "bogus")),
		reporterCalls:     nil,
	}, {
		label:             "broken get k8s svc",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          errMsg("service \"real-name\" not found"),
		wantCode:          http.StatusNotFound,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints("bogus-namespace", testRevName, 1000)),
		svcLister:         serviceLister(service("bogus-namespace", testRevName, "http")),
		reporterCalls:     nil,
	}, {
		label:             "broken get endpoints",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          "",
		wantCode:          http.StatusInternalServerError,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints("bogus-namespace", testRevName, 1000)),
		reporterCalls:     nil,
	}}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			probeResponses := make([]activatortest.FakeResponse, len(test.probeResp))
			for i := 0; i < len(test.probeResp); i++ {
				probeResponses[i] = activatortest.FakeResponse{
					Err:  test.probeErr,
					Code: test.probeCode,
					Body: test.probeResp[i],
				}
			}
			fakeRt := activatortest.FakeRoundTripper{
				ExpectHost:     "test-host",
				ProbeResponses: probeResponses,
				RequestResponse: &activatortest.FakeResponse{
					Err:  test.wantErr,
					Code: test.wantCode,
					Body: test.wantBody,
				},
			}
			reporter := &fakeReporter{}
			params := queue.BreakerParams{QueueDepth: 1000, MaxConcurrency: 1000, InitialCapacity: 0}
			throttler := activator.NewThrottler(
				params,
				test.endpointsInformer,
				sksLister(sks(testNamespace, testRevName)),
				revisionLister(revision(testNamespace, testRevName)),
				TestLogger(t))

			handler := (New(TestLogger(t), reporter, throttler,
				revisionLister(revision(testNamespace, testRevName)),
				serviceLister(service(testNamespace, testRevName, "http")),
				sksLister(sks(testNamespace, testRevName)),
			)).(*activationHandler)
			handler.probeTimeout = test.probeTimeout

			// Setup transports.
			rt := network.RoundTripperFunc(fakeRt.RT)
			handler.transport = rt
			handler.probeTransport = rt

			if test.sksLister != nil {
				handler.sksLister = test.sksLister
			}
			if test.svcLister != nil {
				handler.serviceLister = test.svcLister
			}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, test.namespace)
			req.Header.Set(activator.RevisionHeaderName, test.name)
			req.Host = "test-host"

			// set up config store to populate context
			configStore := setupConfigStore(t)
			ctx := configStore.ToContext(req.Context())

			handler.ServeHTTP(resp, req.WithContext(ctx))

			if resp.Code != test.wantCode {
				t.Errorf("Unexpected response status. Want %d, got %d", test.wantCode, resp.Code)
			}

			gotBody, _ := ioutil.ReadAll(resp.Body)
			if string(gotBody) != test.wantBody {
				t.Errorf("Unexpected response body. Response body %q, want %q", gotBody, test.wantBody)
			}

			if diff := cmp.Diff(test.reporterCalls, reporter.calls); diff != "" {
				t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
			}
		})
	}
}

func TestActivationHandlerProbeCaching(t *testing.T) {
	namespace := testNamespace
	revName := testRevName
	revID := activator.RevisionID{Namespace: namespace, Name: revName}

	fakeRT := activatortest.FakeRoundTripper{}
	rt := network.RoundTripperFunc(fakeRT.RT)

	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	reporter := &fakeReporter{}

	throttler := activator.NewThrottler(
		breakerParams,
		endpointsInformer(endpoints(namespace, revName, breakerParams.InitialCapacity)),
		sksLister(sks(namespace, revName)),
		revisionLister(revision(namespace, revName)),
		TestLogger(t))

	handler := (New(TestLogger(t), reporter, throttler,
		revisionLister(revision(namespace, revName)),
		serviceLister(service(namespace, revName, "http")),
		sksLister(sks(namespace, revName)),
	)).(*activationHandler)

	// Setup transports.
	handler.transport = rt
	handler.probeTransport = rt

	// set up config store to populate context
	configStore := setupConfigStore(t)

	sendRequest(namespace, revName, handler, configStore)
	if fakeRT.NumProbes != 1 {
		t.Errorf("NumProbes = %d, want: %d", fakeRT.NumProbes, 1)
	}

	sendRequest(namespace, revName, handler, configStore)
	// Assert that we didn't reprobe
	if fakeRT.NumProbes != 1 {
		t.Errorf("NumProbes = %d, want: %d", fakeRT.NumProbes, 1)
	}

	// Dropping the capacity to 0 causes the next request to probe again.
	throttler.UpdateCapacity(revID, 0)
	time.AfterFunc(100*time.Millisecond, func() {
		// Asynchronously updating the capacity to let the request through.
		throttler.UpdateCapacity(revID, 1)
	})

	sendRequest(namespace, revName, handler, configStore)
	if fakeRT.NumProbes != 2 {
		t.Errorf("NumProbes = %d, want: %d", fakeRT.NumProbes, 2)
	}
}

// Make sure we return http internal server error when the Breaker is overflowed
func TestActivationHandlerOverflow(t *testing.T) {
	const (
		wantedSuccess = 20
		wantedFailure = 1
		requests      = wantedSuccess + wantedFailure
	)
	respCh := make(chan *httptest.ResponseRecorder, requests)
	namespace := testNamespace
	revName := testRevName

	lockerCh := make(chan struct{})
	fakeRT := activatortest.FakeRoundTripper{
		LockerCh: lockerCh,
		RequestResponse: &activatortest.FakeResponse{
			Code: http.StatusOK,
			Body: wantBody,
		},
	}
	rt := network.RoundTripperFunc(fakeRT.RT)

	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	reporter := &fakeReporter{}

	throttler := activator.NewThrottler(
		breakerParams,
		endpointsInformer(endpoints(namespace, revName, breakerParams.InitialCapacity)),
		sksLister(sks(namespace, revName)),
		revisionLister(revision(namespace, revName)),
		TestLogger(t))

	handler := (New(TestLogger(t), reporter, throttler,
		revisionLister(revision(namespace, revName)),
		serviceLister(service(namespace, revName, "http")),
		sksLister(sks(namespace, revName)),
	)).(*activationHandler)

	// Setup transports.
	handler.transport = rt
	handler.probeTransport = rt

	// set up config store to populate context
	configStore := setupConfigStore(t)

	sendRequests(requests, namespace, revName, respCh, handler, configStore)
	assertResponses(wantedSuccess, wantedFailure, requests, lockerCh, respCh, t)
}

// Make sure if one breaker is overflowed, the requests to other revisions are still served
func TestActivationHandlerOverflowSeveralRevisions(t *testing.T) {
	const (
		wantedSuccess   = 40
		wantedFailure   = 2
		overallRequests = wantedSuccess + wantedFailure
	)

	rev1 := testRevName
	rev2 := testRevNameOther
	revisions := []string{rev1, rev2}

	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	reporter := &fakeReporter{}
	epClient := endpointsInformer(endpoints(testNamespace, rev1, breakerParams.InitialCapacity), endpoints(testNamespace, rev2, breakerParams.InitialCapacity))
	sksClient := sksLister(sks(testNamespace, rev1), sks(testNamespace, rev2))
	revClient := revisionLister(revision(testNamespace, rev1), revision(testNamespace, rev2))
	svcClient := serviceLister(service(testNamespace, rev1, "http"), service(testNamespace, rev2, "http"))

	respCh := make(chan *httptest.ResponseRecorder, overallRequests)
	lockerCh := make(chan struct{})

	throttler := activator.NewThrottler(breakerParams, epClient, sksClient, revClient, TestLogger(t))

	fakeRT := activatortest.FakeRoundTripper{
		LockerCh: lockerCh,
		RequestResponse: &activatortest.FakeResponse{
			Err:  nil,
			Code: http.StatusOK,
			Body: wantBody,
		},
	}
	rt := network.RoundTripperFunc(fakeRT.RT)
	handler := (New(TestLogger(t), reporter, throttler,
		revClient, svcClient, sksClient)).(*activationHandler)

	// Setup transports.
	handler.transport = rt
	handler.probeTransport = rt

	// set up config store to populate context
	configStore := setupConfigStore(t)
	for _, revName := range revisions {
		requestCount := overallRequests / len(revisions)
		sendRequests(requestCount, testNamespace, revName, respCh, handler, configStore)
	}
	assertResponses(wantedSuccess, wantedFailure, overallRequests, lockerCh, respCh, t)
}

func TestActivationHandlerProxyHeader(t *testing.T) {
	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	namespace, revName := testNamespace, testRevName

	interceptCh := make(chan *http.Request, 1)
	rt := network.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		interceptCh <- r
		fake := httptest.NewRecorder()
		return fake.Result(), nil
	})
	throttler := activator.NewThrottler(
		breakerParams,
		endpointsInformer(endpoints(namespace, revName, breakerParams.InitialCapacity)),
		sksLister(sks(namespace, revName)),
		revisionLister(revision(namespace, revName)),
		TestLogger(t))

	fakeRT := activatortest.FakeRoundTripper{
		RequestResponse: &activatortest.FakeResponse{
			Err:  nil,
			Code: http.StatusOK,
			Body: wantBody,
		},
	}
	probeRt := network.RoundTripperFunc(fakeRT.RT)

	handler := &activationHandler{
		transport:      rt,
		probeTransport: probeRt,
		logger:         TestLogger(t),
		reporter:       &fakeReporter{},
		throttler:      throttler,
		revisionLister: revisionLister(revision(testNamespace, testRevName)),
		serviceLister:  serviceLister(service(testNamespace, testRevName, "http")),
		sksLister:      sksLister(sks(testNamespace, testRevName)),
	}

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(activator.RevisionHeaderNamespace, namespace)
	req.Header.Set(activator.RevisionHeaderName, revName)

	// set up config store to populate context
	configStore := setupConfigStore(t)
	ctx := configStore.ToContext(req.Context())
	handler.ServeHTTP(writer, req.WithContext(ctx))

	select {
	case httpReq := <-interceptCh:
		if got := httpReq.Header.Get(network.ProxyHeaderName); got != activator.Name {
			t.Errorf("Header '%s' does not have the expected value. Want = '%s', got = '%s'.", network.ProxyHeaderName, activator.Name, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for a request to be intercepted")
	}
}

func TestActivationHandlerTraceSpans(t *testing.T) {
	testcases := []struct {
		name         string
		wantSpans    int
		traceBackend tracingconfig.BackendType
	}{{
		name:         "zipkin trace enabled",
		wantSpans:    4,
		traceBackend: tracingconfig.Zipkin,
	}, {
		name:         "trace disabled",
		wantSpans:    0,
		traceBackend: tracingconfig.None,
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup transport
			fakeRt := activatortest.FakeRoundTripper{
				RequestResponse: &activatortest.FakeResponse{
					Err:  nil,
					Code: http.StatusOK,
					Body: wantBody,
				},
			}
			rt := network.RoundTripperFunc(fakeRt.RT)

			// Create tracer with reporter recorder
			reporter, co := tracetesting.FakeZipkinExporter()
			defer reporter.Close()
			oct := tracing.NewOpenCensusTracer(co)
			defer oct.Finish()

			cfg := tracingconfig.Config{
				Backend: tc.traceBackend,
				Debug:   true,
			}
			if err := oct.ApplyConfig(&cfg); err != nil {
				t.Errorf("Failed to apply tracer config: %v", err)
			}

			namespace := testNamespace
			revName := testRevName

			breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
			throttler := activator.NewThrottler(
				breakerParams,
				endpointsInformer(endpoints(namespace, revName, breakerParams.InitialCapacity)),
				sksLister(sks(namespace, revName)),
				revisionLister(revision(namespace, revName)),
				TestLogger(t))

			handler := &activationHandler{
				transport:      rt,
				probeTransport: rt,
				logger:         TestLogger(t),
				reporter:       &fakeReporter{},
				throttler:      throttler,
				revisionLister: revisionLister(revision(testNamespace, testRevName)),
				serviceLister:  serviceLister(service(testNamespace, testRevName, "http")),
				sksLister:      sksLister(sks(testNamespace, testRevName)),
			}
			handler.transport = &ochttp.Transport{
				Base: rt,
			}
			handler.probeTransport = rt

			// set up config store to populate context
			configStore := setupConfigStore(t)

			_ = sendRequest(namespace, revName, handler, configStore)

			gotSpans := reporter.Flush()
			if len(gotSpans) != tc.wantSpans {
				t.Errorf("Got %d spans, expected %d", len(gotSpans), tc.wantSpans)
			}

			spanNames := []string{"throttler_try", "probe", "/", "proxy"}
			for i, spanName := range spanNames[0:tc.wantSpans] {
				if gotSpans[i].Name != spanName {
					t.Errorf("Got span %d named %q, expected %q", i, gotSpans[i].Name, spanName)
				}
			}
		})
	}
}

func sendRequest(namespace, revName string, handler *activationHandler, store *activatorconfig.Store) *httptest.ResponseRecorder {
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(activator.RevisionHeaderNamespace, namespace)
	req.Header.Set(activator.RevisionHeaderName, revName)
	ctx := store.ToContext(req.Context())
	handler.ServeHTTP(resp, req.WithContext(ctx))
	return resp
}

// sendRequests sends `count` concurrent requests via the given handler and writes
// the recorded responses to the `respCh`.
func sendRequests(count int, namespace, revName string, respCh chan *httptest.ResponseRecorder, handler *activationHandler, store *activatorconfig.Store) {
	for i := 0; i < count; i++ {
		go func() {
			respCh <- sendRequest(namespace, revName, handler, store)
		}()
	}
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

func (f *fakeReporter) ReportRequestConcurrency(ns, service, config, rev string, v int64) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.calls = append(f.calls, reporterCall{
		Op:        "ReportRequestConcurrency",
		Namespace: ns,
		Service:   service,
		Config:    config,
		Revision:  rev,
		Value:     v,
	})

	return nil
}

func (f *fakeReporter) ReportRequestCount(ns, service, config, rev string, responseCode, numTries int) error {
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
		Value:      1,
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

func revision(namespace, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: "config-" + testRevName,
				serving.ServiceLabelKey:       "service-" + testRevName,
			},
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
			},
		},
	}
}

func revisionLister(revs ...*v1alpha1.Revision) servinglisters.RevisionLister {
	fake := servingfake.NewSimpleClientset()
	informer := servinginformers.NewSharedInformerFactory(fake, 0)
	revisions := informer.Serving().V1alpha1().Revisions()

	for _, rev := range revs {
		fake.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
		revisions.Informer().GetIndexer().Add(rev)
	}

	return revisions.Lister()
}

func service(namespace, name string, portName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name: portName,
				Port: 8080,
			}},
		}}
}

func serviceLister(svcs ...*corev1.Service) corev1listers.ServiceLister {
	fake := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	services := informer.Core().V1().Services()

	for _, svc := range svcs {
		fake.Core().Services(svc.Namespace).Create(svc)
		services.Informer().GetIndexer().Add(svc)
	}

	return services.Lister()
}

func setupConfigStore(t *testing.T) *activatorconfig.Store {
	configStore := activatorconfig.NewStore(TestLogger(t))
	tracingConfig := ConfigMapFromTestFile(t, tracingconfig.ConfigName)
	configStore.OnConfigChanged(tracingConfig)
	return configStore
}

func sks(namespace, name string) *nv1a1.ServerlessService {
	return &nv1a1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: nv1a1.ServerlessServiceStatus{
			// Randomize the test.
			PrivateServiceName: name,
			ServiceName:        helpers.AppendRandomString(name),
		},
	}
}

func sksLister(skss ...*nv1a1.ServerlessService) netlisters.ServerlessServiceLister {
	fake := servingfake.NewSimpleClientset()
	informer := servinginformers.NewSharedInformerFactory(fake, 0)
	services := informer.Networking().V1alpha1().ServerlessServices()

	for _, sks := range skss {
		fake.Networking().ServerlessServices(sks.Namespace).Create(sks)
		services.Informer().GetIndexer().Add(sks)
	}

	return services.Lister()
}

func endpoints(namespace, name string, count int) *corev1.Endpoints {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		}}

	epAddresses := []corev1.EndpointAddress{}
	for i := 1; i <= count; i++ {
		ip := fmt.Sprintf("127.0.0.%v", i)
		epAddresses = append(epAddresses, corev1.EndpointAddress{IP: ip})
	}
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: epAddresses,
	}}

	return ep
}

func endpointsInformer(eps ...*corev1.Endpoints) corev1informers.EndpointsInformer {
	fake := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	endpoints := informer.Core().V1().Endpoints()

	for _, ep := range eps {
		fake.Core().Endpoints(ep.Namespace).Create(ep)
		endpoints.Informer().GetIndexer().Add(ep)
	}

	return endpoints
}

func errMsg(msg string) string {
	return fmt.Sprintf("Error getting active endpoint: %v\n", msg)
}
