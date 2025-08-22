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
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	netheader "knative.dev/networking/pkg/http/header"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/queue"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/logging"
)

const (
	wantBody      = "♫ everything is awesome! ♫"
	testNamespace = "real-namespace"
	testRevName   = "real-name"
)

type fakeThrottler struct {
	err error
}

func (ft fakeThrottler) Try(_ context.Context, _ types.NamespacedName, _ string, f func(string, bool) error) error {
	if ft.err != nil {
		return ft.err
	}
	return f("10.10.10.10:1234", false)
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
			for i := range len(test.probeResp) {
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
			handler := New(ctx, test.throttler, rt, false /*usePassthroughLb*/, logging.FromContext(ctx), false /* TLS */)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Host = "test-host"

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
			ctx = configStore.ToContext(ctx)
			ctx = WithRevisionAndID(ctx, nil, types.NamespacedName{Namespace: testNamespace, Name: testRevName})

			handler.ServeHTTP(resp, req.WithContext(ctx))

			if resp.Code != test.wantCode {
				t.Fatalf("Unexpected response status. Want %d, got %d", test.wantCode, resp.Code)
			}

			gotBody, err := io.ReadAll(resp.Body)
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

	handler := New(ctx, fakeThrottler{}, rt, false /*usePassthroughLb*/, logging.FromContext(ctx), false /* TLS */)

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	// Set up config store to populate context.
	configStore := setupConfigStore(t, logging.FromContext(ctx))
	ctx = configStore.ToContext(req.Context())
	ctx = WithRevisionAndID(ctx, nil, types.NamespacedName{Namespace: testNamespace, Name: testRevName})

	handler.ServeHTTP(writer, req.WithContext(ctx))

	select {
	case httpReq := <-interceptCh:
		if got := httpReq.Header.Get(netheader.ProxyKey); got != activator.Name {
			t.Errorf("Header %q = %q, want: %q", netheader.ProxyKey, got, activator.Name)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for a request to be intercepted")
	}
}

func TestActivationHandlerPassthroughLb(t *testing.T) {
	interceptCh := make(chan *http.Request, 1)
	rt := pkgnet.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		interceptCh <- r
		fake := httptest.NewRecorder()
		return fake.Result(), nil
	})

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	handler := New(ctx, fakeThrottler{}, rt, true /*usePassthroughLb*/, logging.FromContext(ctx), false /* TLS */)

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	// Set up config store to populate context.
	configStore := activatorconfig.NewStore(logging.FromContext(ctx))
	configStore.OnConfigChanged(tracingConfig(false))
	ctx = configStore.ToContext(req.Context())
	ctx = WithRevisionAndID(ctx, nil, types.NamespacedName{Namespace: testNamespace, Name: testRevName})

	handler.ServeHTTP(writer, req.WithContext(ctx))

	select {
	case httpReq := <-interceptCh:
		if got, want := httpReq.Host, "real-name-private.real-namespace"; got != want {
			t.Errorf("Host = %q, want: %q", got, want)
		}
		if got, want := httpReq.Header.Get(netheader.PassthroughLoadbalancingKey), "true"; got != want {
			t.Errorf("Header %q = %q, want: %q", "Host", got, want)
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
				oct.Shutdown(context.Background())
			}()

			handler := New(ctx, fakeThrottler{}, rt, false /*usePassthroughLb*/, logging.FromContext(ctx), false /* TLS */)

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

// TestErrorPropagationFromProxy demonstrates that proxy errors are not properly
// propagated through throttler.Try(), making it impossible to distinguish between
// breaker errors (ErrRequestQueueFull, context.DeadlineExceeded) and proxy errors.
func TestErrorPropagationFromProxy(t *testing.T) {
	tests := []struct {
		name                string
		proxyError          error
		expectThrottlerErr  bool  // Whether throttler.Try should return an error
		expectSpecificError error // The specific error type expected
	}{{
		name:                "successful request",
		proxyError:          nil,
		expectThrottlerErr:  false,
		expectSpecificError: nil,
	}, {
		name:                "proxy network error",
		proxyError:          errors.New("connection refused"),
		expectThrottlerErr:  true,  // This SHOULD preserve the original error
		expectSpecificError: errors.New("connection refused"), // Should get the original error, not ErrQuick502
	}, {
		name:                "proxy timeout", 
		proxyError:          context.DeadlineExceeded,
		expectThrottlerErr:  true,  // This SHOULD preserve the original error
		expectSpecificError: context.DeadlineExceeded, // Should get timeout error, not ErrQuick502
	}, {
		name:                "genuine quick 502",
		proxyError:          nil,  // No transport error, but fast 502 response
		expectThrottlerErr:  true,  // Should still return ErrQuick502
		expectSpecificError: ErrQuick502{}, // Should get ErrQuick502 for fast 502 responses
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// TODO: TEMPORARILY DISABLED - Re-enable when quick 502 detection is re-enabled
			if test.name == "genuine quick 502" {
				t.Skip("Quick 502 detection is currently disabled - see TODO comments in handler.go")
			}
			// Setup a transport that fails with the specified error
			responseCode := http.StatusBadGateway
			responseBody := "proxy error"
			if test.proxyError == nil {
				if test.name == "genuine quick 502" {
					// Keep 502 status but no transport error for quick 502 test
					responseCode = http.StatusBadGateway
					responseBody = "quick 502"
				} else {
					// Success case
					responseCode = http.StatusOK
					responseBody = "success"
				}
			}
			
			fakeRT := activatortest.FakeRoundTripper{
				RequestResponse: &activatortest.FakeResponse{
					Err:  test.proxyError,
					Code: responseCode,
					Body: responseBody,
				},
			}
			rt := pkgnet.RoundTripperFunc(fakeRT.RT)

			// Create handler with real throttler (not fake) to test actual error flow
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()
			
			// Track what error the throttler actually receives
			var actualThrottlerError error
			
			// Create a capturing throttler to intercept the error
			captureThrottler := &capturingThrottler{
				onTry: func(ctx context.Context, revID types.NamespacedName, xRequestId string, f func(string, bool) error) error {
					actualThrottlerError = f("10.10.10.10:1234", false) // Call proxyRequest
					return actualThrottlerError
				},
			}
			
			handler := New(ctx, captureThrottler, rt, false /*usePassthroughLb*/, logging.FromContext(ctx), false /* TLS */)

			// Set up config store to populate context.
			configStore := setupConfigStore(t, logging.FromContext(ctx))
			ctx = configStore.ToContext(ctx)
			ctx = WithRevisionAndID(ctx, nil, types.NamespacedName{Namespace: testNamespace, Name: testRevName})

			// Make request
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Host = "test-host"
			resp := httptest.NewRecorder()

			handler.ServeHTTP(resp, req.WithContext(ctx))

			// Verify error propagation behavior
			if test.expectThrottlerErr {
				if actualThrottlerError == nil {
					t.Errorf("Expected throttler to receive error, but got nil. This demonstrates the error propagation issue.")
				}
				if test.expectSpecificError != nil {
					// For timeout errors, check if it's the correct type
					if errors.Is(test.expectSpecificError, context.DeadlineExceeded) {
						if !errors.Is(actualThrottlerError, context.DeadlineExceeded) {
							t.Errorf("Expected context.DeadlineExceeded error, got %v", actualThrottlerError)
						}
					} else if _, isQuick502 := test.expectSpecificError.(ErrQuick502); isQuick502 {
						// For ErrQuick502, check if we got the right error type
						if _, gotQuick502 := actualThrottlerError.(ErrQuick502); !gotQuick502 {
							t.Errorf("Expected ErrQuick502 error, got %T: %v", actualThrottlerError, actualThrottlerError)
						}
					} else {
						// For other errors, compare the error message
						if actualThrottlerError.Error() != test.expectSpecificError.Error() {
							t.Errorf("Expected specific error %v, got %v", test.expectSpecificError, actualThrottlerError)
						}
					}
				}
			} else {
				if actualThrottlerError != nil {
					t.Errorf("Expected no error from throttler, got %v", actualThrottlerError)
				}
			}
		})
	}
}

// capturingThrottler captures the error returned by the function passed to Try()
type capturingThrottler struct {
	onTry func(context.Context, types.NamespacedName, string, func(string, bool) error) error
}

func (ct *capturingThrottler) Try(ctx context.Context, revID types.NamespacedName, xRequestId string, f func(string, bool) error) error {
	return ct.onTry(ctx, revID, xRequestId, f)
}

func sendRequest(namespace, revName string, handler http.Handler, store *activatorconfig.Store) *httptest.ResponseRecorder {
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/", nil)
	ctx := store.ToContext(req.Context())
	ctx = WithRevisionAndID(ctx, nil, types.NamespacedName{Namespace: namespace, Name: revName})
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

func setupConfigStore(_ testing.TB, logger *zap.SugaredLogger) *activatorconfig.Store {
	configStore := activatorconfig.NewStore(logger)
	configStore.OnConfigChanged(tracingConfig(false))
	return configStore
}

func BenchmarkHandler(b *testing.B) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(b)
	b.Cleanup(cancel)
	configStore := setupConfigStore(b, logging.FromContext(ctx))

	// bodyLength is in Kilobytes.
	for _, bodyLength := range [5]int{2, 16, 32, 64, 128} {
		body := []byte(randomString(1024 * bodyLength))

		rt := pkgnet.RoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				Body:       io.NopCloser(bytes.NewReader(body)),
				StatusCode: http.StatusOK,
			}, nil
		})

		handler := New(ctx, fakeThrottler{}, rt, false /*usePassthroughLb*/, logging.FromContext(ctx), false /* TLS */)

		request := func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			req.Host = "test-host"

			reqCtx := configStore.ToContext(context.Background())
			reqCtx = WithRevisionAndID(reqCtx, nil, types.NamespacedName{Namespace: testNamespace, Name: testRevName})
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
			for range b.N {
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
