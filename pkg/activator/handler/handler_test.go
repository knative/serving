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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.opencensus.io/plugin/ochttp"

	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	activatornet "knative.dev/serving/pkg/activator/net"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	servingv1informers "knative.dev/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"

	. "knative.dev/pkg/configmap/testing"
	. "knative.dev/pkg/logging/testing"
	_ "knative.dev/pkg/system/testing"
)

const (
	wantBody      = "♫ everything is awesome! ♫"
	testNamespace = "real-namespace"
	testRevName   = "real-name"
	testTimeout   = 100 * time.Millisecond
)

func TestActivationHandler(t *testing.T) {
	tests := []struct {
		label         string
		namespace     string
		name          string
		wantBody      string
		wantCode      int
		wantErr       error
		probeErr      error
		probeCode     int
		probeResp     []string
		numBackends   int
		reporterCalls []reporterCall
	}{{
		label:       "active endpoint",
		namespace:   testNamespace,
		name:        testRevName,
		wantBody:    wantBody,
		wantCode:    http.StatusOK,
		wantErr:     nil,
		numBackends: 5,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   1,
			Value:      1,
		}},
	}, {
		label:         "no active endpoint",
		namespace:     "fake-namespace",
		name:          "fake-name",
		wantBody:      errMsg("revision.serving.knative.dev \"fake-name\" not found"),
		wantCode:      http.StatusNotFound,
		wantErr:       nil,
		numBackends:   0,
		reporterCalls: nil,
	}, {
		label:       "request error",
		namespace:   testNamespace,
		name:        testRevName,
		wantBody:    "request error\n",
		wantCode:    http.StatusBadGateway,
		wantErr:     errors.New("request error"),
		numBackends: 10,
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusBadGateway,
			Attempts:   1,
			Value:      1,
		}},
	}, {
		label:         "broken get k8s svc",
		namespace:     testNamespace,
		name:          testRevName,
		wantBody:      context.DeadlineExceeded.Error() + "\n",
		wantCode:      http.StatusServiceUnavailable,
		wantErr:       nil,
		reporterCalls: nil,
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
			rt := network.RoundTripperFunc(fakeRt.RT)

			reporter := &fakeReporter{}
			params := queue.BreakerParams{QueueDepth: 1000, MaxConcurrency: 1000, InitialCapacity: 0}

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer func() {
				cancel()
				ClearAll()
			}()
			revisionInformer(ctx, revision(testNamespace, testRevName))
			endpointsInformer(ctx,
				endpoints(testNamespace, testRevName, params.InitialCapacity, networking.ServicePortNameHTTP1))

			throttler := activatornet.NewThrottler(ctx, params)
			throttler.SetCapacity(testNamespace, testRevName, test.numBackends, "" /*clusterIP*/)

			handler := (New(ctx, throttler, reporter)).(*activationHandler)

			// Setup transports.
			handler.transport = rt

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, test.namespace)
			req.Header.Set(activator.RevisionHeaderName, test.name)
			req.Host = "test-host"

			// Timeout context
			tryContext, cancel := context.WithTimeout(ctx, testTimeout)
			defer cancel()

			// Set up config store to populate context.
			configStore := setupConfigStore(t)
			ctx = configStore.ToContext(tryContext)

			handler.ServeHTTP(resp, req.WithContext(ctx))

			if resp.Code != test.wantCode {
				t.Errorf("Unexpected response status. Want %d, got %d", test.wantCode, resp.Code)
			}

			gotBody, _ := ioutil.ReadAll(resp.Body)
			if string(gotBody) != test.wantBody {
				t.Errorf("Unexpected response body. Response body %q, want %q", gotBody, test.wantBody)
			}

			// Filter out response time reporter calls
			var gotCalls []reporterCall
			if reporter.calls != nil {
				gotCalls = make([]reporterCall, 0)
				for _, gotCall := range reporter.calls {
					if gotCall.Op != "ReportResponseTime" {
						gotCalls = append(gotCalls, gotCall)
					}
				}
			}

			if diff := cmp.Diff(test.reporterCalls, gotCalls); diff != "" {
				t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
			}

		})
	}
}

func TestActivationHandlerProxyHeader(t *testing.T) {
	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}

	interceptCh := make(chan *http.Request, 1)
	rt := network.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		interceptCh <- r
		fake := httptest.NewRecorder()
		return fake.Result(), nil
	})

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer func() {
		cancel()
		ClearAll()
	}()
	endpointsInformer(ctx,
		endpoints(testNamespace, testRevName, breakerParams.InitialCapacity, networking.ServicePortNameHTTP1))
	revisionInformer(ctx, revision(testNamespace, testRevName))

	throttler := activatornet.NewThrottler(ctx, breakerParams)
	throttler.SetCapacity(testNamespace, testRevName, 5, "129.0.0.1:2112")

	handler := (New(ctx, throttler, &fakeReporter{})).(*activationHandler)
	handler.transport = rt

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(activator.RevisionHeaderNamespace, testNamespace)
	req.Header.Set(activator.RevisionHeaderName, testRevName)

	// Set up config store to populate context.
	configStore := setupConfigStore(t)
	ctx = configStore.ToContext(req.Context())
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
		wantSpans:    3,
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
			oct := tracing.NewOpenCensusTracer(co)

			cfg := tracingconfig.Config{
				Backend: tc.traceBackend,
				Debug:   true,
			}
			if err := oct.ApplyConfig(&cfg); err != nil {
				t.Errorf("Failed to apply tracer config: %v", err)
			}

			breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer func() {
				cancel()
				ClearAll()
				reporter.Close()
				oct.Finish()
			}()
			revisions := revisionInformer(ctx, revision(testNamespace, testRevName))
			endpoints := endpointsInformer(ctx,
				endpoints(testNamespace, testRevName,
					breakerParams.InitialCapacity, networking.ServicePortNameHTTP1))

			controller.StartInformers(ctx.Done(), revisions.Informer(), endpoints.Informer())
			throttler := activatornet.NewThrottler(ctx, breakerParams)

			throttler.SetCapacity(testNamespace, testRevName,
				breakerParams.InitialCapacity, "129.0.0.1:1234")

			handler := (New(ctx, throttler, &fakeReporter{})).(*activationHandler)
			handler.transport = &ochttp.Transport{
				Base: rt,
			}

			// Set up config store to populate context.
			configStore := setupConfigStore(t)
			sendRequest(testNamespace, testRevName, handler, configStore)

			gotSpans := reporter.Flush()
			if len(gotSpans) != tc.wantSpans {
				t.Errorf("Got %d spans, expected %d", len(gotSpans), tc.wantSpans)
			}

			spanNames := []string{"throttler_try", "/", "proxy"}
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
			RevisionSpec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
			},
		},
	}
}

func revisionInformer(ctx context.Context, revs ...*v1alpha1.Revision) servingv1informers.RevisionInformer {
	fake := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	for _, rev := range revs {
		fake.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
		revisions.Informer().GetIndexer().Add(rev)
	}

	return revisions
}

func setupConfigStore(t *testing.T) *activatorconfig.Store {
	configStore := activatorconfig.NewStore(TestLogger(t))
	tracingConfig := ConfigMapFromTestFile(t, tracingconfig.ConfigName)
	configStore.OnConfigChanged(tracingConfig)
	return configStore
}

func endpoints(namespace, name string, count int, portName string) *corev1.Endpoints {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				serving.RevisionUID:       "fake-uid",
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
				serving.RevisionLabelKey:  name,
			},
		}}

	epAddresses := []corev1.EndpointAddress{}
	for i := 1; i <= count; i++ {
		ip := fmt.Sprintf("127.0.0.%v", i)
		epAddresses = append(epAddresses, corev1.EndpointAddress{IP: ip})
	}
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: epAddresses,
		Ports: []corev1.EndpointPort{{
			Name: portName,
			Port: 1234,
		}},
	}}

	return ep
}

func endpointsInformer(ctx context.Context, eps ...*corev1.Endpoints) corev1informers.EndpointsInformer {
	fake := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)

	for _, ep := range eps {
		fake.CoreV1().Endpoints(ep.Namespace).Create(ep)
		endpoints.Informer().GetIndexer().Add(ep)
	}
	return endpoints
}

func errMsg(msg string) string {
	return fmt.Sprintf("Error getting active endpoint: %v\n", msg)
}
