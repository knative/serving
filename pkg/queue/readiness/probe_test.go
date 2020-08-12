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
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	p := NewProbe(v1p)

	if diff := cmp.Diff(p.Probe, v1p); diff != "" {
		t.Errorf("NewProbe (-want, +got) = %v", diff)
	}

	if c := p.count; c != 0 {
		t.Errorf("Expected Probe.Count == 0, got: %d", c)
	}
}

func TestTCPFailure(t *testing.T) {
	pb := NewProbe(&corev1.Probe{
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
	})

	if pb.ProbeContainer() {
		t.Error("Reported success when no server was available for connection")
	}
}

func TestEmptyHandler(t *testing.T) {
	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   1,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler:          corev1.Handler{},
	})

	if pb.ProbeContainer() {
		t.Error("Reported success when no handler was configured.")
	}
}

func TestExecHandler(t *testing.T) {
	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   1,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{"echo", "hello"},
			}},
	})

	if pb.ProbeContainer() {
		t.Error("Expected ExecProbe to always fail")
	}
}

func TestTCPSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	t.Log("Port", tsURL.Port())

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: tsURL.Hostname(),
				Port: intstr.FromString(tsURL.Port()),
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Probe report failure. Expected success.")
	}
}

func TestHTTPFailureToConnect(t *testing.T) {
	pb := NewProbe(&corev1.Probe{
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
	})

	if pb.ProbeContainer() {
		t.Error("Reported success when no server was available for connection")
	}
}

func TestHTTPBadResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   5,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

	if pb.ProbeContainer() {
		t.Error("Reported success when server replied with Bad Request")
	}
}

func TestHTTPSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   5,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Probe failed. Expected success.")
	}
}

func TestHTTPManyParallel(t *testing.T) {
	var count atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Inc() == 1 {
			// Add a small amount of work to allow the requests below to collapse into one.
			time.Sleep(200 * time.Millisecond)
		}
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   5,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

	var grp errgroup.Group
	for i := 0; i < 2; i++ {
		grp.Go(func() error {
			if !pb.ProbeContainer() {
				return errors.New("failed to probe container")
			}
			return nil
		})
	}
	if err := grp.Wait(); err != nil {
		t.Error("Probe failed. Expected success.")
	}

	// This should trigger a second probe now.
	if !pb.ProbeContainer() {
		t.Error("Probe failed. Expected success.")
	}
	if got, want := count.Load(), int32(2); got != want {
		t.Errorf("Probe count = %d, want: %d", got, want)
	}
}

func TestHTTPTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

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

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Probe failed. Wanted success.")
	}
}

func TestKnHTTPSuccessWithRetry(t *testing.T) {
	var count atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fail the very first request.
		if count.Inc() == 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Probe failed. Expected success after retry.")
	}
}

func TestKnHTTPSuccessWithThreshold(t *testing.T) {
	var threshold int32 = 3

	var count atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Inc()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: threshold,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Expected success after second attempt.")
	}

	if count.Load() < threshold {
		t.Errorf("Expected %d requests before reporting success", threshold)
	}
}

func TestKnHTTPSuccessWithThresholdAndFailure(t *testing.T) {
	var threshold int32 = 3
	var requestFailure int32 = 2

	var count atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Inc() == requestFailure {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: threshold,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host: tsURL.Hostname(),
				Port: intstr.FromString(tsURL.Port()),
				HTTPHeaders: []corev1.HTTPHeader{{
					Name:  "Test-key",
					Value: "Test-value",
				}},
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Expected success.")
	}

	if count.Load() < threshold+requestFailure {
		t.Errorf("Wanted %d requests before reporting success, got=%d", threshold+requestFailure, count)
	}
}

func TestKnHTTPTimeoutFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", ts.URL, err)
	}

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   tsURL.Hostname(),
				Port:   intstr.FromString(tsURL.Port()),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	})
	pb.pollTimeout = time.Second
	var logs bytes.Buffer
	pb.out = &logs

	if pb.ProbeContainer() {
		t.Errorf("Probe succeeded. Expected failure due to timeout. Logs:\n%s", logs.String())
	}
}

func TestKnTCPProbeSuccess(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal("Error setting up tcp listener:", err)
	}
	defer listener.Close()
	addr := listener.Addr().(*net.TCPAddr)

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(addr.Port),
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Got probe error. Wanted success.")
	}
}

func TestKnUnimplementedProbe(t *testing.T) {
	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 1,
		FailureThreshold: 0,
		Handler:          corev1.Handler{},
	})

	if pb.ProbeContainer() {
		t.Error("Got probe success. Wanted failure.")
	}
}
func TestKnTCPProbeFailure(t *testing.T) {
	pb := NewProbe(&corev1.Probe{
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
	})
	pb.pollTimeout = time.Second
	var logs bytes.Buffer
	pb.out = &logs

	if pb.ProbeContainer() {
		t.Errorf("Got probe success. Wanted failure. Logs:\n%s", logs.String())
	}
}

func TestKnTCPProbeSuccessWithThreshold(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal("Error setting up tcp listener:", err)
	}
	defer listener.Close()
	addr := listener.Addr().(*net.TCPAddr)

	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: 3,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(addr.Port),
			},
		},
	})

	if !pb.ProbeContainer() {
		t.Error("Got probe error. Wanted success.")
	}

	if got := pb.count; got < 3 {
		t.Errorf("Count = %d, want: 3", got)
	}
}

func TestKnTCPProbeSuccessThresholdIncludesFailure(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal("Error setting up tcp listener:", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	var successThreshold int32 = 3
	pb := NewProbe(&corev1.Probe{
		PeriodSeconds:    0,
		TimeoutSeconds:   0,
		SuccessThreshold: successThreshold,
		FailureThreshold: 0,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(addr.Port),
			},
		},
	})

	connCount := 0
	const desiredConnCount = 4 // 1 conn from 1st server, 3 from 2nd server

	errChan := make(chan bool, 1)
	go func() {
		errChan <- pb.ProbeContainer()
	}()

	if _, err = listener.Accept(); err != nil {
		t.Fatal("Failed to accept TCP conn:", err)
	}
	connCount++

	// Close server and sleep to give probe time to fail a few times
	// and reset count
	listener.Close()
	time.Sleep(500 * time.Millisecond)

	listener2, err := net.Listen("tcp", fmt.Sprintf(":%d", addr.Port))
	if err != nil {
		t.Fatal("Error setting up tcp listener:", err)
	}

	for {
		if connCount < desiredConnCount {
			if _, err = listener2.Accept(); err != nil {
				t.Fatal("Failed to accept TCP conn:", err)
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
	if got := pb.count; got < successThreshold {
		t.Errorf("Count = %d, want: %d", got, successThreshold)
	}
}
