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
	expectedHeader := corev1.HTTPHeader{
		Name:  "Testkey",
		Value: "Testval",
	}
	examples := []struct {
		name           string
		setPath        string
		expectedHeader corev1.HTTPHeader
		expectedPath   string
		expectedQuery  string
	}{{
		name:           "Path with leading slash",
		setPath:        "/health",
		expectedHeader: expectedHeader,
		expectedQuery:  "foo=bar",
		expectedPath:   "/health",
	}, {
		name:           "Path with no leading slash",
		setPath:        "health",
		expectedHeader: expectedHeader,
		expectedQuery:  "foo=bar",
		expectedPath:   "/health",
	}}

	for _, e := range examples {
		var gotPath string
		var gotQuery string
		var gotHeader corev1.HTTPHeader
		var gotKubeletHeader bool
		t.Run(e.name, func(t *testing.T) {
			configPath := e.setPath + "?" + e.expectedQuery
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if v := r.Header.Get(e.expectedHeader.Name); v != "" {
					gotHeader = corev1.HTTPHeader{Name: e.expectedHeader.Name, Value: v}
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
			action.HTTPHeaders = []corev1.HTTPHeader{e.expectedHeader}
			config := HTTPProbeConfigOptions{
				Timeout:       time.Second,
				HTTPGetAction: action,
				MaxProtoMajor: 1,
			}

			// Connecting to the server should work
			if err := HTTPProbe(config); err != nil {
				t.Error("Expected probe to succeed but it failed with", err)
			}
			if d := cmp.Diff(gotHeader, e.expectedHeader); d != "" {
				t.Error("Expected probe headers to match; diff:\n", d)
			}
			if !gotKubeletHeader {
				t.Error("Expected kubelet probe header to be added to request")
			}
			if !cmp.Equal(gotPath, e.expectedPath) {
				t.Errorf("Path = %s, want: %s", gotPath, e.expectedPath)
			}
			if !cmp.Equal(gotQuery, e.expectedQuery) {
				t.Errorf("Query = %s, want: %s", gotQuery, e.expectedQuery)
			}
			// Close the server so probing fails afterwards.
			server.Close()
			if err := HTTPProbe(config); err == nil {
				t.Error("Expected probe to fail but it didn't")
			}
		})
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

func TestHTTP2ProtocolProbe(t *testing.T) {
	tests := []struct {
		name          string
		enableHTTP1   bool
		enableH2C     bool
		statusHTTP1   int
		statusHTTP2   int
		expectedProto int
		expectError   bool
	}{
		{
			name:          "server supports H2C only and ready",
			enableHTTP1:   false,
			enableH2C:     true,
			statusHTTP1:   0,
			statusHTTP2:   http.StatusOK,
			expectedProto: 2,
			expectError:   false,
		},
		{
			name:          "server supports HTTP/1 only and ready",
			enableHTTP1:   true,
			enableH2C:     false,
			statusHTTP1:   http.StatusOK,
			statusHTTP2:   0,
			expectedProto: 1,
			expectError:   false,
		},
		{
			name:          "server supports both H2C and HTTP/1 (should prefer H2C)",
			enableHTTP1:   true,
			enableH2C:     true,
			statusHTTP1:   http.StatusOK,
			statusHTTP2:   http.StatusOK,
			expectedProto: 2,
			expectError:   false,
		},
		{
			name:          "server supports H2C (status 503) and HTTP/1 (status 200)",
			enableHTTP1:   true,
			enableH2C:     true,
			statusHTTP1:   http.StatusOK,
			statusHTTP2:   http.StatusServiceUnavailable,
			expectedProto: 1,
			expectError:   false,
		},
		{
			name:          "server supports HTTP/1 but returns not ready (503)",
			enableHTTP1:   true,
			enableH2C:     false,
			statusHTTP1:   http.StatusServiceUnavailable,
			expectedProto: 0,
			expectError:   true,
		},
		{
			name:          "server supports both but returns not ready (500)",
			enableHTTP1:   true,
			enableH2C:     true,
			statusHTTP1:   http.StatusInternalServerError,
			statusHTTP2:   http.StatusInternalServerError,
			expectedProto: 0,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup server with specified protocols
			protocols := &http.Protocols{}
			protocols.SetHTTP1(tt.enableHTTP1)
			protocols.SetUnencryptedHTTP2(tt.enableH2C)

			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status := tt.statusHTTP1
				if r.ProtoMajor == 2 {
					status = tt.statusHTTP2
				}
				if status > 0 {
					w.WriteHeader(status)
				}
			}))
			server.Config.Protocols = protocols
			server.Start()
			t.Cleanup(server.Close)

			action := newHTTPGetAction(t, server.URL)
			config := HTTPProbeConfigOptions{
				Timeout:       time.Second,
				HTTPGetAction: action,
				KubeMajor:     "1",
				KubeMinor:     "28",
			}

			proto, err := detectHTTPProtocolVersion(config)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if proto != tt.expectedProto {
				t.Errorf("Expected proto %d, got %d", tt.expectedProto, proto)
			}
		})
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
