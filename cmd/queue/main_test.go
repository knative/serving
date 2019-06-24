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

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
	logtesting "knative.dev/pkg/logging/testing"
)

const wantHost = "a-better-host.com"

func TestHandlerReqEvent(t *testing.T) {
	var httpHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(activator.RevisionHeaderName) != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.Header.Get(activator.RevisionHeaderNamespace) != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if got, want := r.Host, wantHost; got != want {
			t.Errorf("Host header = %q, want: %q", got, want)
		}
		if got, want := r.Header.Get(network.OriginalHostHeader), ""; got != want {
			t.Errorf("%s header was preserved", network.OriginalHostHeader)
		}

		w.WriteHeader(http.StatusOK)
	}

	server := httptest.NewServer(httpHandler)
	serverURL, _ := url.Parse(server.URL)

	defer server.Close()
	proxy := httputil.NewSingleHostReverseProxy(serverURL)

	params := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	breaker := queue.NewBreaker(params)
	reqChan := make(chan queue.ReqEvent, 10)
	h := handler(reqChan, breaker, proxy)

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	// Verify the Original host header processing.
	req.Host = "nimporte.pas"
	req.Header.Set(network.OriginalHostHeader, wantHost)

	req.Header.Set(network.ProxyHeaderName, activator.Name)
	h(writer, req)
	select {
	case e := <-reqChan:
		if e.EventType != queue.ProxiedIn {
			t.Errorf("Want: %v, got: %v\n", queue.ReqIn, e.EventType)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for an event to be intercepted")
	}
}

func TestProberHandler(t *testing.T) {
	defer logtesting.ClearAll()
	logger = logtesting.TestLogger(t)

	// All arguments are needed only for serving.
	h := handler(nil, nil, nil)

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	req.Header.Set(network.ProbeHeaderName, "test-probe")
	req.Header.Set(network.ProxyHeaderName, activator.Name)

	h(writer, req)

	// Should get 400.
	if got, want := writer.Code, http.StatusBadRequest; got != want {
		t.Errorf("Bad probe status = %v, want: %v", got, want)
	}
	if got, want := strings.TrimSpace(writer.Body.String()), fmt.Sprintf(badProbeTemplate, "test-probe"); got != want {
		// \r\n might be inserted, etc.
		t.Errorf("Bad probe body = %q, want: %q, diff: %s", got, want, cmp.Diff(got, want))
	}

	// Fix up the header.
	writer = httptest.NewRecorder()
	req.Header.Set(network.ProbeHeaderName, queue.Name)

	server := httptest.NewServer(http.HandlerFunc(h))
	defer server.Close()
	userTargetAddress = strings.TrimPrefix(server.URL, "http://")
	h(writer, req)

	// Should get 200.
	if got, want := writer.Code, http.StatusOK; got != want {
		t.Errorf("Good probe status = %v, want: %v", got, want)
	}

	// Body should be the `queue`.
	if got, want := strings.TrimSpace(writer.Body.String()), queue.Name; got != want {
		// \r\n might be inserted, etc.
		t.Errorf("Good probe body = %q, want: %q, diff: %s", got, want, cmp.Diff(got, want))
	}
}

func TestCreateVarLogLink(t *testing.T) {
	dir, err := ioutil.TempDir("", "TestCreateVarLogLink")
	if err != nil {
		t.Errorf("Failed to created temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)

	createVarLogLink("default", "service-7f97f9465b-5kkm5", "user-container", "knative-var-log", dir)

	source := path.Join(dir, "default_service-7f97f9465b-5kkm5_user-container")
	want := "../knative-var-log"
	got, err := os.Readlink(source)
	if err != nil {
		t.Errorf("Failed to read symlink: %v", err)
	}
	if got != want {
		t.Errorf("Incorrect symlink = %q, want %q, diff: %s", got, want, cmp.Diff(got, want))
	}
}

func TestProbeQueueConnectionFailure(t *testing.T) {
	port := 12345 // some random port (that's not listening)
	if err := probeQueueHealthPath(port, time.Second); err == nil {
		t.Error("Expected error, got nil")
	}
}

func TestProbeQueueNotReady(t *testing.T) {
	queueProbed := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queueProbed = true
		w.WriteHeader(http.StatusBadRequest)
	}))

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("Failed to convert port(%s) to int: %v", u.Port(), err)
	}

	err = probeQueueHealthPath(port, 1*time.Second)

	if diff := cmp.Diff(err.Error(), "probe returned not ready"); diff != "" {
		t.Errorf("Unexpected not ready message: %s", diff)
	}

	if !queueProbed {
		t.Errorf("Expected the queue proxy server to be probed")
	}
}

func TestProbeQueueReady(t *testing.T) {
	queueProbed := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queueProbed = true
		w.WriteHeader(http.StatusOK)
	}))

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("Failed to convert port(%s) to int: %v", u.Port(), err)
	}

	if err = probeQueueHealthPath(port, 1*time.Second); err != nil {
		t.Errorf("probeQueueHealthPath(%d) = %s", port, err)
	}

	if !queueProbed {
		t.Errorf("Expected the queue proxy server to be probed")
	}
}

func TestProbeQueueDelayedReady(t *testing.T) {
	count := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count < 9 {
			w.WriteHeader(http.StatusBadRequest)
			count++
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("Failed to convert port(%s) to int: %v", u.Port(), err)
	}

	if err := probeQueueHealthPath(port, time.Second); err != nil {
		t.Errorf("probeQueueHealthPath(%d) = %s", port, err)
	}
}

func TestTCPFailure(t *testing.T) {
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   1,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(12345),
			},
		},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Reported success when no server was available for connection")
	}
}

func TestEmptyHandler(t *testing.T) {
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   1,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler:          corev1.Handler{},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Reported success when no server was available for connection")
	}
}

func TestExecHandler(t *testing.T) {
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   1,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{"echo", "hello"},
			}},
	}, t)

	if !pb.ProbeContainer() {
		t.Error("Probe failed. Expected Exec probe to always pass.")
	}
}

func TestTCPSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromString(port),
			},
		},
	}, t)

	if !pb.ProbeContainer() {
		t.Error("Probe report failure. Expected success.")
	}
}

func TestParseProbeSuccess(t *testing.T) {
	logger = logtesting.TestLogger(t)
	expectedProbe := &corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromString("8080"),
			},
		},
	}
	probeBytes, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatalf("Failed to parse probe %#v", err)
	}
	gotProbe, err := parseProbe(string(probeBytes))
	if err != nil {
		t.Fatalf("Failed parseProbe %#v", err)
	}
	if d := cmp.Diff(gotProbe.Probe, expectedProbe); d != "" {
		t.Errorf("probe diff %s", d)
	}
}

func TestParseProbeFailure(t *testing.T) {
	logger = logtesting.TestLogger(t)

	probeBytes, err := json.Marshal("wrongProbeObject")
	if err != nil {
		t.Fatalf("Failed to parse probe %#v", err)
	}
	_, err = parseProbe(string(probeBytes))
	if err == nil {
		t.Fatal("Expected parseProbe() to fail")
	}
}

func TestHTTPFailureToConnect(t *testing.T) {
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Port:   intstr.FromInt(12345),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Reported success when no server was available for connection")
	}
}

func TestHTTPBadResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   5,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Port:   intstr.FromString(port),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Reported success when server replied with Bad Request")
	}
}

func TestHTTPSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   5,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Port:   intstr.FromString(port),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if !pb.ProbeContainer() {
		t.Error("Probe failed. Expected success.")
	}
}

func TestHTTPTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host: "127.0.0.1",
				Port: intstr.FromString(port),
			},
		},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Probe succeeded. Expected failure due to timeout.")
	}
}

func TestHTTPSuccessWithDelay(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Port:   intstr.FromString(port),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if !pb.ProbeContainer() {
		t.Error("Probe failed. Wanted success.")
	}
}

func TestKNHTTPSuccessWithRetry(t *testing.T) {
	attempted := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !attempted {
			w.WriteHeader(http.StatusBadRequest)
			attempted = true
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Port:   intstr.FromString(port),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if !pb.ProbeContainer() {
		t.Error("Probe failed. Expected success after retry.")
	}
}

func TestKNHTTPSuccessWithThreshold(t *testing.T) {
	var count int32
	var threshold int32 = 3

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: threshold,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Port:   intstr.FromString(port),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if !pb.ProbeContainer() {
		t.Error("Expected success after second attempt.")
	}

	if count != threshold {
		t.Errorf("Expected %d requests before reporting success", threshold)
	}
}

func TestKNHTTPSuccessWithThresholdAndFailure(t *testing.T) {
	var count int32
	var threshold int32 = 3
	var requestFailure int32 = 2

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++

		if count == requestFailure {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: threshold,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host: "127.0.0.1",
				Port: intstr.FromString(port),
				HTTPHeaders: []corev1.HTTPHeader{{
					Name:  "Test-key",
					Value: "Test-value",
				}},
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if !pb.ProbeContainer() {
		t.Error("Expected success.")
	}

	if count != threshold+requestFailure {
		t.Errorf("Wanted %d requests before reporting success, got=%d", threshold+requestFailure, count)
	}
}

func TestKNHTTPTimeoutFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")

	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Port:   intstr.FromString(port),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Probe succeeded. Expected failure due to timeout.")
	}
}

func TestKNativeTCPProbeSuccess(t *testing.T) {
	port := freePort(t)
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(port),
			},
		},
	}, t)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("Error setting up tcp listener: %s", err.Error())
	}
	defer listener.Close()

	if !pb.ProbeContainer() {
		t.Error("Got probe error. Wanted success.")
	}
}

func TestKNativeUnimplementedProbe(t *testing.T) {
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler:          corev1.Handler{},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Got probe success. Wanted failure.")
	}
}
func TestKNativeTCPProbeFailure(t *testing.T) {
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(12345),
			},
		},
	}, t)

	if pb.ProbeContainer() {
		t.Error("Got probe success. Wanted failure.")
	}
}

func TestKNativeTCPProbeSuccessWithThreshold(t *testing.T) {
	port := freePort(t)
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 3,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(port),
			},
		},
	}, t)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("Error setting up tcp listener: %s", err.Error())
	}
	defer listener.Close()

	if !pb.ProbeContainer() {
		t.Error("Got probe error. Wanted success.")
	}

	if pb.Count() != 3 {
		t.Errorf("Expected count to be 3, go %d", pb.Count())
	}
}

func TestKNativeTCPProbeSuccessThresholdIncludesFailure(t *testing.T) {
	var successThreshold int32 = 3
	port := freePort(t)
	pb := newProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: successThreshold,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(port),
			},
		},
	}, t)

	connCount := 0
	desiredConnCount := 4 // 1 conn from 1st server, 3 from 2nd server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("Error setting up tcp listener: %s", err.Error())
	}

	errChan := make(chan bool, 1)
	go func(errChan chan bool) {
		errChan <- pb.ProbeContainer()
	}(errChan)

	if _, err = listener.Accept(); err != nil {
		t.Fatalf("Failed to accept TCP conn: %s", err.Error())
	}
	connCount++

	// Close server and sleep to give probe time to fail a few times
	// and reset count
	listener.Close()
	time.Sleep(500 * time.Millisecond)

	listener2, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("Error setting up tcp listener: %s", err.Error())
	}

	for {
		if connCount < desiredConnCount {
			if _, err = listener2.Accept(); err != nil {
				t.Fatalf("Failed to accept TCP conn: %s", err.Error())
			}
			connCount++
		} else {
			listener2.Close()
			break
		}
	}

	if probeErr := <-errChan; !probeErr {
		t.Error("Wanted ProbeContainer() successed but got error")
	}
	if pb.Count() != successThreshold {
		t.Errorf("Expected count to be %d but got %d", successThreshold, pb.Count())
	}
}

func freePort(t *testing.T) int {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Error setting up tcp listener: %s", err.Error())
	}
	listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func newProbe(pb *corev1.Probe, t *testing.T) probe {
	return probe{
		Probe:  pb,
		count:  0,
		logger: logtesting.TestLogger(t),
	}
}
