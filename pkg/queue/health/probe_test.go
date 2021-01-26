/*
Copyright 2019 The Knative Authors

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

package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	network "knative.dev/networking/pkg"
	apicfg "knative.dev/serving/pkg/apis/config"
)

func TestTCPProbe(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	config := TCPProbeConfigOptions{
		Address:       server.Listener.Addr().String(),
		SocketTimeout: time.Second,
	}
	// Connecting to the server should work
	if err := TCPProbe(config); err != nil {
		t.Error("Probe failed with:", err)
	}

	// Close the server so probing fails afterwards
	server.Close()
	if err := TCPProbe(config); err == nil {
		t.Error("Expected probe to fail but it didn't")
	}
}

func TestHTTPProbeSuccess(t *testing.T) {
	var gotHeader corev1.HTTPHeader
	var gotKubeletHeader bool
	expectedHeader := corev1.HTTPHeader{
		Name:  "Testkey",
		Value: "Testval",
	}
	var gotPath string
	expectedPath := "/health"
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		for headerKey, headerValue := range r.Header {
			// Filtering for expectedHeader.TestKey to avoid other HTTP probe headers
			if expectedHeader.Name == headerKey {
				gotHeader = corev1.HTTPHeader{Name: headerKey, Value: headerValue[0]}
			}

			if headerKey == "User-Agent" && strings.HasPrefix(headerValue[0], network.KubeProbeUAPrefix) {
				gotKubeletHeader = true
			}
		}

		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	action := newHTTPGetAction(t, server.URL)
	action.Path = expectedPath
	action.HTTPHeaders = []corev1.HTTPHeader{expectedHeader}

	config := HTTPProbeConfigOptions{
		Timeout:       time.Second,
		HTTPGetAction: action,
	}
	// Connecting to the server should work
	if err := HTTPProbe(context.Background(), config); err != nil {
		t.Error("Expected probe to succeed but it failed with", err)
	}
	if d := cmp.Diff(gotHeader, expectedHeader); d != "" {
		t.Error("Expected probe headers to match but got", d)
	}
	if !gotKubeletHeader {
		t.Error("Expected kubelet probe header to be added to request")
	}
	if !cmp.Equal(gotPath, expectedPath) {
		t.Errorf("Expected %s path to match but got %s", expectedPath, gotPath)
	}
	// Close the server so probing fails afterwards
	server.Close()
	if err := HTTPProbe(context.Background(), config); err == nil {
		t.Error("Expected probe to fail but it didn't")
	}
}

func TestHTTPProbeNoAutoHTTP2IfDisabled(t *testing.T) {
	ctx := context.Background()
	cfg := apicfg.FromContextOrDefaults(ctx)
	cfg.Features.AutoDetectHTTP2 = apicfg.Disabled
	ctx = apicfg.ToContext(ctx, cfg)

	h2cHeaders := map[string]string{
		"Connection": "Upgrade, HTTP2-Settings",
		"Upgrade":    "h2c",
	}
	expectedPath := "/health"
	var callCount int32
	callCount = 0

	server := newH2cTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			// This is the h2c handshake, we won't do anything.
			for key, value := range h2cHeaders {
				if r.Header.Get(key) == value {
					t.Errorf("Key %v = %v was NOT supposed to be present in the request", key, value)
				}
			}
		} else {
			t.Errorf("Handler should only have one calls, this is call %d", count)
		}
	})

	action := newHTTPGetAction(t, server.URL)
	action.Path = expectedPath

	config := HTTPProbeConfigOptions{
		Timeout:       time.Second,
		HTTPGetAction: action,
	}
	if err := HTTPProbe(ctx, config); err != nil {
		t.Error("Expected probe to succeed but it failed with", err)
	}
	if count := atomic.LoadInt32(&callCount); count != 1 {
		t.Errorf("Unexpected call count %d", count)
	}
}

func TestHTTPProbeAutoHTTP2(t *testing.T) {
	ctx := context.Background()
	cfg := apicfg.FromContextOrDefaults(ctx)
	cfg.Features.AutoDetectHTTP2 = apicfg.Enabled
	ctx = apicfg.ToContext(ctx, cfg)

	h2cHeaders := map[string]string{
		"Connection": "Upgrade, HTTP2-Settings",
		"Upgrade":    "h2c",
	}
	expectedPath := "/health"
	var callCount int32
	callCount = 0

	server := newH2cTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			// This is the h2c handshake, we won't do anything.
			for key, value := range h2cHeaders {
				if r.Header.Get(key) != value {
					t.Errorf("Key %v = %v was supposed to be present in the request", key, value)
				}
			}
		} else if count == 2 {
			// This is the expected call. It should not have any of the h2c upgrade stuff, since the h2c test server will handle that for us.
			for key, value := range h2cHeaders {
				if r.Header.Get(key) == value {
					t.Errorf("Key %v = %v was NOT supposed to be present in the request", key, value)
				}
			}
		} else {
			t.Errorf("Handler should only have two calls, this is call %d", count)
		}
	})

	action := newHTTPGetAction(t, server.URL)
	action.Path = expectedPath

	config := HTTPProbeConfigOptions{
		Timeout:       time.Second,
		HTTPGetAction: action,
	}
	if err := HTTPProbe(ctx, config); err != nil {
		t.Error("Expected probe to succeed but it failed with", err)
	}
	if count := atomic.LoadInt32(&callCount); count != 2 {
		t.Errorf("Unexpected call count %d", count)
	}
}

func TestHTTPSchemeProbeSuccess(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := HTTPProbeConfigOptions{
		Timeout:       time.Second,
		HTTPGetAction: newHTTPGetAction(t, server.URL),
	}

	// Connecting to the server should work
	if err := HTTPProbe(context.Background(), config); err != nil {
		t.Error("Expected probe to succeed but failed with error", err)
	}

	// Close the server so probing fails afterwards
	server.Close()
	if err := HTTPProbe(context.Background(), config); err == nil {
		t.Error("Expected probe to fail but it didn't")
	}
}

func TestHTTPProbeTimeoutFailure(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(1 * time.Second):
		case <-r.Context().Done():
		}

		w.WriteHeader(http.StatusOK)
	})

	config := HTTPProbeConfigOptions{
		Timeout:       1 * time.Millisecond,
		HTTPGetAction: newHTTPGetAction(t, server.URL),
	}
	if err := HTTPProbe(context.Background(), config); err == nil {
		t.Error("Expected probe to fail but it succeeded")
	}
}

func TestHTTPProbeResponseStatusCodeFailure(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	config := HTTPProbeConfigOptions{
		Timeout:       time.Second,
		HTTPGetAction: newHTTPGetAction(t, server.URL),
	}
	if err := HTTPProbe(context.Background(), config); err == nil {
		t.Error("Expected probe to fail but it succeeded")
	}
}

func TestHTTPProbeResponseErrorFailure(t *testing.T) {
	config := HTTPProbeConfigOptions{
		HTTPGetAction: newHTTPGetAction(t, "http://localhost:0"),
	}
	if err := HTTPProbe(context.Background(), config); err == nil {
		t.Error("Expected probe to fail but it succeeded")
	}
}

func TestIsHTTPProbeShuttingDown(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantResult bool
	}{{
		name:       "statusCode: 410",
		statusCode: 410,
		wantResult: true,
	}, {
		name:       "statusCode: 503",
		statusCode: 503,
		wantResult: false,
	}, {
		name:       "statusCode: 200",
		statusCode: 200,
		wantResult: false,
	}, {
		name:       "statusCode: 301",
		statusCode: 301,
		wantResult: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := http.Response{StatusCode: test.statusCode}
			result := IsHTTPProbeShuttingDown(&response)
			if result != test.wantResult {
				t.Errorf("IsHTTPProbeShuttingDown returned unexpected result: got %v want %v",
					result, test.wantResult)
			}
		})
	}
}

func newH2cTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	h2s := &http2.Server{}
	t.Helper()
	server := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
	}), h2s))
	t.Cleanup(server.Close)

	return server
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	return server
}

func newHTTPGetAction(t *testing.T, serverURL string) *corev1.HTTPGetAction {
	t.Helper()

	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("Error parsing URL")
	}

	return &corev1.HTTPGetAction{
		Host: u.Hostname(),
		Port: intstr.FromString(u.Port()),
		// We only ever use httptest.NewServer which is http.
		Scheme: corev1.URISchemeHTTP,
	}
}
