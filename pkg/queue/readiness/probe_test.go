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

package readiness

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logtesting "knative.dev/pkg/logging/testing"
)

func TestNewProbe(t *testing.T) {
	v1p := &corev1.Probe{
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
	}

	logger := logtesting.TestLogger(t)

	p := NewProbe(v1p, logger)

	if diff := cmp.Diff(p.Probe, v1p); diff != "" {
		t.Errorf("NewProbe (-want, +got) = %v", diff)
	}

	if c := p.Count(); c != 0 {
		t.Errorf("Expected Probe.Count == 0, got: %d", c)
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
		t.Error("Reported success when no handler was configured.")
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

	if pb.ProbeContainer() {
		t.Error("Expected ExecProbe to always fail")
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

func TestKnHTTPSuccessWithRetry(t *testing.T) {
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

func TestKnHTTPSuccessWithThreshold(t *testing.T) {
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

func TestKnHTTPSuccessWithThresholdAndFailure(t *testing.T) {
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

func TestKnHTTPTimeoutFailure(t *testing.T) {
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

func TestKnTCPProbeSuccess(t *testing.T) {
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

func TestKnUnimplementedProbe(t *testing.T) {
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
func TestKnTCPProbeFailure(t *testing.T) {
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

func TestKnTCPProbeSuccessWithThreshold(t *testing.T) {
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

func TestKnTCPProbeSuccessThresholdIncludesFailure(t *testing.T) {
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

func newProbe(pb *corev1.Probe, t *testing.T) Probe {
	return Probe{
		Probe:  pb,
		count:  0,
		logger: logtesting.TestLogger(t),
	}
}
