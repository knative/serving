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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.opencensus.io/plugin/ochttp"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	pkgnet "knative.dev/pkg/network"
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
		name:      "active endpoint",
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
		name:      "request error",
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
		name:          "throttler timeout",
		wantBody:      context.DeadlineExceeded.Error() + "\n",
		wantCode:      http.StatusServiceUnavailable,
		wantErr:       nil,
		throttler:     fakeThrottler{err: context.DeadlineExceeded},
		reporterCalls: nil,
	}, {
		name:          "overflow",
		wantBody:      "activator overload\n",
		wantCode:      http.StatusServiceUnavailable,
		wantErr:       nil,
		throttler:     fakeThrottler{err: anet.ErrActivatorOverload},
		reporterCalls: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probeResponses := make([]activatortest.FakeResponse, len(test.probeResp))
			for i := 0; i < len(test.probeResp); i++ {
				probeResponses[i] = activatortest.FakeResponse{
					Err:  test.probeErr,
					Code: test.probeCode,
					Body: test.probeResp[i],
				}
			}
			fakeRT := activatortest.FakeRoundTripper{
				ExpectHost:     "test-host",
				ProbeResponses: probeResponses,
				RequestResponse: &activatortest.FakeResponse{
					Err:  test.wantErr,
					Code: test.wantCode,
					Body: test.wantBody,
				},
			}
			rt := pkgnet.RoundTripperFunc(fakeRT.RT)

			reporter := &fakeReporter{}

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()
			handler := (New(ctx, test.throttler, reporter)).(*activationHandler)

			// Setup transports.
			handler.transport = rt

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Host = "test-host"

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
			ctx = configStore.ToContext(ctx)
			ctx = withRevision(ctx, revision(testNamespace, testRevName))
			ctx = withRevID(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRevName})

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
	rt := pkgnet.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		interceptCh <- r
		fake := httptest.NewRecorder()
		return fake.Result(), nil
	})

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	handler := (New(ctx, fakeThrottler{}, &fakeReporter{})).(*activationHandler)
	handler.transport = rt

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	// Set up config store to populate context.
	configStore := setupConfigStore(t, logging.FromContext(ctx))
	ctx = configStore.ToContext(req.Context())
	ctx = withRevision(ctx, revision(testNamespace, testRevName))
	ctx = withRevID(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRevName})
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
			fakeRT := activatortest.FakeRoundTripper{
				RequestResponse: &activatortest.FakeResponse{
					Err:  nil,
					Code: http.StatusOK,
					Body: wantBody,
				},
			}
			rt := pkgnet.RoundTripperFunc(fakeRT.RT)

			// Create tracer with reporter recorder
			reporter, co := tracetesting.FakeZipkinExporter()
			oct := tracing.NewOpenCensusTracer(co)

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "config-tracing",
				},
				Data: map[string]string{
					"zipkin-endpoint": "localhost:1234",
					"backend":         string(tc.traceBackend),
					"debug":           "true",
				},
			}
			cfg, err := tracingconfig.NewTracingConfigFromConfigMap(cm)
			if err != nil {
				t.Fatalf("Failed to generate config: %v", err)
			}
			if err := oct.ApplyConfig(cfg); err != nil {
				t.Errorf("Failed to apply tracer config: %v", err)
			}

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer func() {
				cancel()
				reporter.Close()
				oct.Finish()
			}()

			handler := (New(ctx, fakeThrottler{}, &fakeReporter{})).(*activationHandler)
			handler.transport = rt
			handler.tracingTransport = &ochttp.Transport{Base: rt}

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
			// Update the store with our "new" config explicitly.
			configStore.OnConfigChanged(cm)
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
	ctx := store.ToContext(req.Context())
	ctx = withRevision(ctx, revision(namespace, revName))
	ctx = withRevID(ctx, types.NamespacedName{Namespace: namespace, Name: revName})
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

func setupConfigStore(t *testing.T, logger *zap.SugaredLogger) *activatorconfig.Store {
	configStore := activatorconfig.NewStore(logger)
	tracingConfig := ConfigMapFromTestFile(t, tracingconfig.ConfigName)
	configStore.OnConfigChanged(tracingConfig)
	return configStore
}

func BenchmarkHandler(b *testing.B) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(&testing.T{})
	defer cancel()
	configStore := setupConfigStore(&testing.T{}, logging.FromContext(ctx))

	// bodyLength is in kilobytes.
	for _, bodyLength := range [5]int{2, 16, 32, 64, 128} {
		body := []byte(randomString(1024 * bodyLength))

		rt := pkgnet.RoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				Body:       ioutil.NopCloser(bytes.NewReader(body)),
				StatusCode: http.StatusOK,
			}, nil
		})

		handler := (New(ctx, fakeThrottler{}, &fakeReporter{})).(*activationHandler)
		handler.transport = rt

		request := func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			req.Host = "test-host"

			reqCtx := configStore.ToContext(context.Background())
			reqCtx = withRevision(reqCtx, revision(testNamespace, testRevName))
			reqCtx = withRevID(reqCtx, types.NamespacedName{Namespace: testNamespace, Name: testRevName})
			return req.WithContext(reqCtx)
		}

		test := func(req *http.Request, b *testing.B) {
			resp := &responseRecorder{}
			handler.ServeHTTP(resp, req)
			if resp.code != http.StatusOK {
				b.Fatalf("resp.Code = %d, want: StatusOK(200)", resp.code)
			}
			if got, want := resp.size, int32(len(body)); got != want {
				b.Fatalf("|body| = %d, want = %d", got, want)
			}
		}

		b.Run(fmt.Sprintf("%03dk-resp-len-sequential", bodyLength), func(b *testing.B) {
			req := request()
			for j := 0; j < b.N; j++ {
				test(req, b)
			}
		})

		b.Run(fmt.Sprintf("%03dk-resp-len-parallel", bodyLength), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				req := request()
				for pb.Next() {
					test(req, b)
				}
			})
		})
	}
}

func randomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

// responseRecorder is an implementation of http.ResponseWriter and http.Flusher
// that captures the response code and size.
type responseRecorder struct {
	code int
	size int32
}

func (rr *responseRecorder) Flush() {}

func (rr *responseRecorder) Header() http.Header {
	return http.Header{}
}

func (rr *responseRecorder) Write(p []byte) (int, error) {
	atomic.AddInt32(&rr.size, int32(len(p)))
	return ioutil.Discard.Write(p)
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.code = code
}
