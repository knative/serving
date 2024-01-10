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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/atomic"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	netheader "knative.dev/networking/pkg/http/header"
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
	var (
		gotHeader        corev1.HTTPHeader
		gotKubeletHeader bool
	)
	expectedHeader := corev1.HTTPHeader{
		Name:  "Testkey",
		Value: "Testval",
	}
	var gotPath string
	var gotQuery string
	const expectedPath = "/health"
	const expectedQuery = "foo=bar"
	const configPath = expectedPath + "?" + expectedQuery
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get(expectedHeader.Name); v != "" {
			gotHeader = corev1.HTTPHeader{Name: expectedHeader.Name, Value: v}
		}
		if v := r.Header.Get(netheader.UserAgentKey); strings.HasPrefix(v, netheader.KubeProbeUAPrefix) {
			gotKubeletHeader = true
		}
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	})

	action := newHTTPGetAction(t, server.URL)
	action.Path = configPath
	action.HTTPHeaders = []corev1.HTTPHeader{expectedHeader}

	config := HTTPProbeConfigOptions{
		Timeout:       time.Second,
		HTTPGetAction: action,
		MaxProtoMajor: 1,
	}

	// Connecting to the server should work
	if err := HTTPProbe(config); err != nil {
		t.Error("Expected probe to succeed but it failed with", err)
	}
	if d := cmp.Diff(gotHeader, expectedHeader); d != "" {
		t.Error("Expected probe headers to match; diff:\n", d)
	}
	if !gotKubeletHeader {
		t.Error("Expected kubelet probe header to be added to request")
	}
	if !cmp.Equal(gotPath, expectedPath) {
		t.Errorf("Path = %s, want: %s", gotPath, expectedPath)
	}
	if !cmp.Equal(gotQuery, expectedQuery) {
		t.Errorf("Query = %s, want: %s", gotQuery, expectedQuery)
	}
	// Close the server so probing fails afterwards.
	server.Close()
	if err := HTTPProbe(config); err == nil {
		t.Error("Expected probe to fail but it didn't")
	}
}

func TestHTTPProbeNoAutoHTTP2IfDisabled(t *testing.T) {
	h2cHeaders := map[string]string{
		"Connection": "Upgrade, HTTP2-Settings",
		"Upgrade":    "h2c",
	}
	expectedPath := "/health"

	var callCount atomic.Int32
	server := newH2cTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Inc()
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
		MaxProtoMajor: 1,
	}
	if err := HTTPProbe(config); err != nil {
		t.Error("Expected probe to succeed but it failed with", err)
	}
	if count := callCount.Load(); count != 1 {
		t.Errorf("Unexpected call count %d", count)
	}
}

func TestHTTPProbeAutoHTTP2(t *testing.T) {
	t.Skip("The test and the underlying behavior needs hardening, see #10962")

	h2cHeaders := map[string]string{
		"Connection": "Upgrade, HTTP2-Settings",
		"Upgrade":    "h2c",
	}
	expectedPath := "/health"
	var callCount atomic.Int32

	server := newH2cTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Inc()
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
		MaxProtoMajor: 0,
	}
	if err := HTTPProbe(config); err != nil {
		t.Error("Expected probe to succeed but it failed with", err)
	}
	if count := callCount.Load(); count != 2 {
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
		MaxProtoMajor: 1,
	}

	// Connecting to the server should work
	if err := HTTPProbe(config); err != nil {
		t.Error("Expected probe to succeed but failed with error", err)
	}

	// Close the server so probing fails afterwards
	server.Close()
	if err := HTTPProbe(config); err == nil {
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
		MaxProtoMajor: 1,
	}
	if err := HTTPProbe(config); err == nil {
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
		MaxProtoMajor: 1,
	}
	if err := HTTPProbe(config); err == nil {
		t.Error("Expected probe to fail but it succeeded")
	}
}

func TestHTTPProbeResponseErrorFailure(t *testing.T) {
	config := HTTPProbeConfigOptions{
		HTTPGetAction: newHTTPGetAction(t, "http://localhost:0"),
		MaxProtoMajor: 1,
	}
	if err := HTTPProbe(config); err == nil {
		t.Error("Expected probe to fail but it succeeded")
	}
}

func TestGRPCProbeSuccess(t *testing.T) {
	// use ephemeral port to prevent port conflict
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(s, &grpcHealthServer{})

	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Serve(lis)
	}()

	assignedPort := lis.Addr().(*net.TCPAddr).Port
	gRPCAction := newGRPCAction(t, assignedPort)
	config := GRPCProbeConfigOptions{
		Timeout:    time.Second,
		GRPCAction: gRPCAction,
	}

	if err := GRPCProbe(config); err != nil {
		t.Error("Expected probe to succeed but it failed with", err)
	}

	// explicitly stop grpc server
	s.Stop()

	if grpcServerErr := <-errChan; grpcServerErr != nil {
		t.Fatalf("Failed to run gRPC test server %v", grpcServerErr)
	}
	close(errChan)
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
		t.Fatal("Error parsing URL")
	}

	return &corev1.HTTPGetAction{
		Host: u.Hostname(),
		Port: intstr.FromString(u.Port()),
		// We only ever use httptest.NewServer which is http.
		Scheme: corev1.URISchemeHTTP,
	}
}

func newGRPCAction(t *testing.T, port int) *corev1.GRPCAction {
	t.Helper()

	return &corev1.GRPCAction{
		Port: int32(port),
	}
}

type grpcHealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
}

func (s *grpcHealthServer) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}
