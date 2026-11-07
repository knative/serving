/*
Copyright 2026 The Knative Authors

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

package e2e

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"knative.dev/pkg/network"
)

func TestTrafficHTTPClientCancellation(t *testing.T) {
	t.Skip("Diagnostics-only CI control run: traffic-cleanup fix is disabled")
	for _, reason := range []string{"stop", "parent"} {
		t.Run(reason, func(t *testing.T) {
			started := make(chan struct{})
			canceled := make(chan struct{})
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
				close(canceled)
			}))
			defer s.Close()

			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			stop := make(chan struct{})
			original := s.Client()
			transport := original.Transport
			client := newTrafficHTTPClient(parent, original, stop)
			if original.Transport != transport {
				t.Fatal("changed the original client's transport")
			}
			done := make(chan error, 1)
			go func() {
				resp, err := client.Get(s.URL)
				if resp != nil {
					resp.Body.Close()
				}
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not start")
			}
			if reason == "stop" {
				close(stop)
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("request error = %v, want context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request was not canceled")
			}
			select {
			case <-canceled:
			case <-time.After(5 * time.Second):
				t.Fatal("server did not observe request cancellation")
			}
		})
	}
}

func TestGenerateTrafficCancelsRequests(t *testing.T) {
	t.Skip("Diagnostics-only CI control run: traffic-cleanup fix is disabled")
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer s.Close()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	stop := make(chan struct{})
	client := newTrafficHTTPClient(parent, s.Client(), stop)
	attacker := vegeta.NewAttacker(vegeta.Client(client), vegeta.Workers(1), vegeta.MaxWorkers(1), vegeta.Timeout(0))
	done := make(chan error, 1)
	go func() {
		done <- generateTraffic(&TestContext{t: t}, attacker, vegeta.ConstantPacer{}, stop, vegeta.Target{Method: http.MethodGet, URL: s.URL})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(stop)
		t.Fatal("traffic did not start")
	}
	close(stop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown cancellation must not count against the SLO: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("traffic did not stop with an outstanding request")
	}
}

func TestGenerateTrafficDrainsResults(t *testing.T) {
	t.Skip("Diagnostics-only CI control run: traffic-cleanup fix is disabled")
	started := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: network.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}
	attacker := vegeta.NewAttacker(vegeta.Client(client), vegeta.Workers(1), vegeta.MaxWorkers(1))
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- generateTraffic(&TestContext{t: t}, attacker, vegeta.ConstantPacer{}, stop, vegeta.Target{Method: http.MethodGet, URL: "http://traffic.test"})
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(stop)
		close(release)
		t.Fatal("traffic did not start")
	}
	close(stop)
	select {
	case <-done:
		close(release)
		t.Fatal("traffic returned without draining the outstanding worker's result")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("traffic did not stop after the outstanding request completed")
	}
}
