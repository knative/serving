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
	config := TCPProbeConfigOptions{
		Address:       server.Listener.Addr().String(),
		SocketTimeout: time.Second,
	}
	// Connecting to the server should work
	if err := TCPProbe(config); err != nil {
		t.Errorf("Expected probe to succeed but it failed with %v", err)
	}

	// Close the server so probing fails afterwards
	server.Close()
	if err := TCPProbe(config); err == nil {
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
	config := HTTPProbeConfigOptions{
		Headers: []corev1.HTTPHeader{expectedHeader},
		Timeout: time.Second,
		URL:     server.URL,
	}
	// Connecting to the server should work
	if err := HTTPProbe(config); err != nil {
		t.Errorf("Expected probe to succeed but it failed with %v", err)
	}
	if d := cmp.Diff(gotHeader, expectedHeader); d != "" {
		t.Errorf("Expected probe headers to match but got %s", d)
	}
	// Close the server so probing fails afterwards
	server.Close()
	if err := HTTPProbe(config); err == nil {
		t.Error("Expected probe to fail but it didn't")
	}
}

func TestHTTPProbeTimeoutFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	config := HTTPProbeConfigOptions{
		Timeout: time.Second,
		URL:     server.URL,
	}
	if err := HTTPProbe(config); err == nil {
		t.Error("Expected probe to fail but it successded")
	}
}

func TestHTTPProbeResponseStatusCodeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	config := HTTPProbeConfigOptions{
		Timeout: time.Second,
		URL:     server.URL,
	}
	if err := HTTPProbe(config); err == nil {
		t.Error("Expected probe to fail but it successded")
	}
}

func TestHTTPProbeResponseErrorFailure(t *testing.T) {
	config := HTTPProbeConfigOptions{
		URL: "http://localhost:0",
	}
	if err := HTTPProbe(config); err == nil {
		t.Error("Expected probe to fail but it successded")
	}
}
