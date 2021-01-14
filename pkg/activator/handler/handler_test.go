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
	"testing"
	"time"

	"go.uber.org/atomic"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	network "knative.dev/networking/pkg"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/queue"

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

func (ft fakeThrottler) Try(_ context.Context, _ types.NamespacedName, f func(string) error) error {
	if ft.err != nil {
		return ft.err
	}
	return f("10.10.10.10:1234")
}

func TestActivationHandler(t *testing.T) {
	tests := []struct {
		name      string
		wantBody  string
		wantCode  int
		wantErr   error
		probeErr  error
		probeCode int
		probeResp []string
		throttler Throttler
	}{{
		name:      "active endpoint",
		wantBody:  wantBody,
		wantCode:  http.StatusOK,
		throttler: fakeThrottler{},
	}, {
		name:      "request error",
		wantBody:  "request error\n",
		wantCode:  http.StatusBadGateway,
		wantErr:   errors.New("request error"),
		throttler: fakeThrottler{},
	}, {
		name:      "throttler timeout",
		wantBody:  context.DeadlineExceeded.Error() + "\n",
		wantCode:  http.StatusServiceUnavailable,
		throttler: fakeThrottler{err: context.DeadlineExceeded},
	}, {
		name:      "overflow",
		wantBody:  "pending request queue full\n",
		wantCode:  http.StatusServiceUnavailable,
		throttler: fakeThrottler{err: queue.ErrRequestQueueFull},
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

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()
			handler := New(ctx, test.throttler, rt)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Host = "test-host"

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
			ctx = configStore.ToContext(ctx)
			ctx = util.WithRevID(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRevName})

			handler.ServeHTTP(resp, req.WithContext(ctx))

			if resp.Code != test.wantCode {
				t.Fatalf("Unexpected response status. Want %d, got %d", test.wantCode, resp.Code)
			}

			gotBody, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatal("Error reading body:", err)
			}
			if string(gotBody) != test.wantBody {
				t.Errorf("Response body = %q, want: %q", gotBody, test.wantBody)
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

	handler := New(ctx, fakeThrottler{}, rt)

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	// Set up config store to populate context.
	configStore := setupConfigStore(t, logging.FromContext(ctx))
	ctx = configStore.ToContext(req.Context())
	ctx = util.WithRevID(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRevName})

	handler.ServeHTTP(writer, req.WithContext(ctx))

	select {
	case httpReq := <-interceptCh:
		if got := httpReq.Header.Get(network.ProxyHeaderName); got != activator.Name {
			t.Errorf("Header %q = %q, want: %q", network.ProxyHeaderName, got, activator.Name)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for a request to be intercepted")
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
		traceBackend: tracingconfig.None,
	}}

	spanNames := []string{"throttler_try", "/", "activator_proxy"}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup transport
			fakeRT := activatortest.FakeRoundTripper{
				RequestResponse: &activatortest.FakeResponse{
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
					Name: tracingconfig.ConfigName,
				},
				Data: map[string]string{
					"zipkin-endpoint": "localhost:1234",
					"backend":         string(tc.traceBackend),
					"debug":           "true",
				},
			}
			cfg, err := tracingconfig.NewTracingConfigFromConfigMap(cm)
			if err != nil {
				t.Fatal("Failed to generate config:", err)
			}
			if err := oct.ApplyConfig(cfg); err != nil {
				t.Error("Failed to apply tracer config:", err)
			}

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer func() {
				cancel()
				reporter.Close()
				oct.Finish()
			}()

			handler := New(ctx, fakeThrottler{}, rt)

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
			// Update the store with our "new" config explicitly.
			configStore.OnConfigChanged(cm)
			sendRequest(testNamespace, testRevName, handler, configStore)

			gotSpans := reporter.Flush()
			if len(gotSpans) != tc.wantSpans {
				t.Errorf("NumSpans = %d, want: %d", len(gotSpans), tc.wantSpans)
			}

			for i, spanName := range spanNames[0:tc.wantSpans] {
				if gotSpans[i].Name != spanName {
					t.Errorf("Span[%d] = %q, expected %q", i, gotSpans[i].Name, spanName)
				}
			}
		})
	}
}

func sendRequest(namespace, revName string, handler http.Handler, store *activatorconfig.Store) *httptest.ResponseRecorder {
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/", nil)
	ctx := store.ToContext(req.Context())
	ctx = util.WithRevID(ctx, types.NamespacedName{Namespace: namespace, Name: revName})
	handler.ServeHTTP(resp, req.WithContext(ctx))
	return resp
}

func revision(namespace, name string) *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: "config-" + name,
				serving.ServiceLabelKey:       "service-" + name,
			},
		},
		Spec: v1.RevisionSpec{
			ContainerConcurrency: ptr.Int64(1),
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
	b.Cleanup(cancel)
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

		handler := New(ctx, fakeThrottler{}, rt)

		request := func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			req.Host = "test-host"

			reqCtx := configStore.ToContext(context.Background())
			reqCtx = util.WithRevID(reqCtx, types.NamespacedName{Namespace: testNamespace, Name: testRevName})
			return req.WithContext(reqCtx)
		}

		test := func(req *http.Request, b *testing.B) {
			resp := &responseRecorder{}
			handler.ServeHTTP(resp, req)
			if resp.code != http.StatusOK {
				b.Fatalf("resp.Code = %d, want: StatusOK(200)", resp.code)
			}
			if got, want := resp.size.Load(), int32(len(body)); got != want {
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
	letter := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

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
	size atomic.Int32
}

func (rr *responseRecorder) Header() http.Header {
	return http.Header{}
}

func (rr *responseRecorder) Write(p []byte) (int, error) {
	rr.size.Add(int32(len(p)))
	return len(p), nil
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.code = code
}
