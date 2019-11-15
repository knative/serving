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
	"go.uber.org/zap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	anet "knative.dev/serving/pkg/activator/net"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	servingv1informers "knative.dev/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	"knative.dev/serving/pkg/network"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "knative.dev/pkg/configmap/testing"
	"knative.dev/pkg/logging"
	_ "knative.dev/pkg/system/testing"
)

const (
	wantBody      = "♫ everything is awesome! ♫"
	testNamespace = "real-namespace"
	testRevName   = "real-name"
)

type fakeThrottler struct {
	err error
}

func (ft fakeThrottler) Try(ctx context.Context, _ types.NamespacedName, f func(string) error) error {
	if ft.err != nil {
		return ft.err
	}
	return f("10.10.10.10:1234")
}

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
		throttler     Throttler
		reporterCalls []reporterCall
	}{{
		label:     "active endpoint",
		namespace: testNamespace,
		name:      testRevName,
		wantBody:  wantBody,
		wantCode:  http.StatusOK,
		wantErr:   nil,
		throttler: fakeThrottler{},
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
		label:         "unknown revision",
		namespace:     "fake-namespace",
		name:          "fake-name",
		wantBody:      errMsg(`revision.serving.knative.dev "fake-name" not found`),
		wantCode:      http.StatusNotFound,
		wantErr:       nil,
		throttler:     fakeThrottler{},
		reporterCalls: nil,
	}, {
		label:     "request error",
		namespace: testNamespace,
		name:      testRevName,
		wantBody:  "request error\n",
		wantCode:  http.StatusBadGateway,
		wantErr:   errors.New("request error"),
		throttler: fakeThrottler{},
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
		label:         "throttler timeout",
		namespace:     testNamespace,
		name:          testRevName,
		wantBody:      context.DeadlineExceeded.Error() + "\n",
		wantCode:      http.StatusServiceUnavailable,
		wantErr:       nil,
		throttler:     fakeThrottler{err: context.DeadlineExceeded},
		reporterCalls: nil,
	}, {
		label:         "overflow",
		namespace:     testNamespace,
		name:          testRevName,
		wantBody:      "activator overload\n",
		wantCode:      http.StatusServiceUnavailable,
		wantErr:       nil,
		throttler:     fakeThrottler{err: anet.ErrActivatorOverload},
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

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()
			revisionInformer(ctx, revision(testNamespace, testRevName))
			handler := (New(ctx, test.throttler, reporter)).(*activationHandler)

			// Setup transports.
			handler.transport = rt

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, test.namespace)
			req.Header.Set(activator.RevisionHeaderName, test.name)
			req.Host = "test-host"

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
			ctx = configStore.ToContext(ctx)

			handler.ServeHTTP(resp, req.WithContext(ctx))

			if resp.Code != test.wantCode {
				t.Fatalf("Unexpected response status. Want %d, got %d", test.wantCode, resp.Code)
			}

			gotBody, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Error reading body: %v", err)
			}
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
	interceptCh := make(chan *http.Request, 1)
	rt := network.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		interceptCh <- r
		fake := httptest.NewRecorder()
		return fake.Result(), nil
	})

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()
	revisionInformer(ctx, revision(testNamespace, testRevName))

	handler := (New(ctx, fakeThrottler{}, &fakeReporter{})).(*activationHandler)
	handler.transport = rt

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(activator.RevisionHeaderNamespace, testNamespace)
	req.Header.Set(activator.RevisionHeaderName, testRevName)

	// Set up config store to populate context.
	configStore := setupConfigStore(t, logging.FromContext(ctx))
	ctx = configStore.ToContext(req.Context())
	handler.ServeHTTP(writer, req.WithContext(ctx))

	select {
	case httpReq := <-interceptCh:
		if got := httpReq.Header.Get(network.ProxyHeaderName); got != activator.Name {
			t.Errorf("Header %q = %q, want:  %q", network.ProxyHeaderName, got, activator.Name)
		}
	case <-time.After(1 * time.Second):
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

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			revisions := revisionInformer(ctx, revision(testNamespace, testRevName))
			waitInformers, err := controller.RunInformers(ctx.Done(), revisions.Informer())
			if err != nil {
				t.Fatalf("Failed to start informers: %v", err)
			}
			defer func() {
				cancel()
				reporter.Close()
				oct.Finish()
				waitInformers()
			}()

			handler := (New(ctx, fakeThrottler{}, &fakeReporter{})).(*activationHandler)
			handler.transport = &ochttp.Transport{
				Base: rt,
			}

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
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

func setupConfigStore(t *testing.T, logger *zap.SugaredLogger) *activatorconfig.Store {
	configStore := activatorconfig.NewStore(logger)
	tracingConfig := ConfigMapFromTestFile(t, tracingconfig.ConfigName)
	configStore.OnConfigChanged(tracingConfig)
	return configStore
}

func errMsg(msg string) string {
	return fmt.Sprintf("Error getting active endpoint: %v\n", msg)
}
