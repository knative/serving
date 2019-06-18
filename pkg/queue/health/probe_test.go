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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
)

func TestTCPProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	serverAddr := server.Listener.Addr().String()

	// Connecting to the server should work
	if err := TCPProbe(serverAddr, 1*time.Second); err != nil {
		t.Errorf("Expected probe to succeed but it failed with %v", err)
	}

	// Close the server so probing fails afterwards
	server.Close()
	if err := TCPProbe(serverAddr, 1*time.Second); err == nil {
		t.Error("Expected probe to fail but it didn't")
	}
}

func TestHTTPProbeSuccess(t *testing.T) {
	var gotHeader corev1.HTTPHeader
	expectedHeader := corev1.HTTPHeader{
		Name:  "Testkey",
		Value: "Testval",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for headerKey, headerValue := range r.Header {
			// Flitering for expectedHeader.TestKey to avoid other HTTP probe headers
			if expectedHeader.Name == headerKey {
				gotHeader = corev1.HTTPHeader{Name: headerKey, Value: headerValue[0]}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	serverAddr := server.URL

	// Connecting to the server should work
	if err := HTTPProbe(serverAddr, []corev1.HTTPHeader{expectedHeader}, 1*time.Second); err != nil {
		t.Errorf("Expected probe to succeed but it failed with %v", err)
	}
	if d := cmp.Diff(gotHeader, expectedHeader); d != "" {
		t.Errorf("Expected probe headers to match but got %s", d)
	}
	// Close the server so probing fails afterwards
	server.Close()
	if err := HTTPProbe(serverAddr, []corev1.HTTPHeader{expectedHeader}, 1*time.Second); err == nil {
		t.Error("Expected probe to fail but it didn't")
	}
}

func TestHTTPProbeTimeoutFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	serverAddr := server.URL

	if err := HTTPProbe(serverAddr, nil, 1*time.Second); err == nil {
		t.Error("Expected probe to fail but it successded")
	}
}

func TestHTTPProbeResponseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	serverAddr := server.URL

	if err := HTTPProbe(serverAddr, nil, 1*time.Second); err == nil {
		t.Error("Expected probe to fail but it successded")
	}
}
