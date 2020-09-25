/*
Copyright 2020 The Knative Authors

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

package queue

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	network "knative.dev/networking/pkg"
	"knative.dev/serving/pkg/activator"
)

const (
	wantHost        = "a-better-host.com"
	reportingPeriod = time.Second
)

func TestHandlerBreakerQueueFull(t *testing.T) {
	// This test sends two requests, ensuring queue
	// is saturated. Third will return immediately.
	resp := make(chan struct{})
	blockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-resp
	})
	breaker := NewBreaker(BreakerParams{
		QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1,
	})
	stats := network.NewRequestStats(time.Now())

	h := ProxyHandler(breaker, stats, false /*tracingEnabled*/, blockHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:8081/time", nil)
	if err != nil {
		t.Fatal("NewRequestWithContext =", err)
	}
	var (
		eg      errgroup.Group
		barrier sync.WaitGroup
	)
	barrier.Add(2)
	eg.Go(func() error {
		barrier.Done()
		h(httptest.NewRecorder(), req)
		return nil
	})
	eg.Go(func() error {
		barrier.Done()
		h(httptest.NewRecorder(), req)
		return nil
	})
	barrier.Wait()
	// Now we know the queue is full next should exit immediately.

	eg.Go(func() error {
		defer close(resp) // Make the other requests terminate.
		rec := httptest.NewRecorder()
		h(rec, req)
		if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
			return fmt.Errorf("Code = %d, want: %d", got, want)
		}
		const want = "pending request queue full"
		if got := rec.Body.String(); !strings.Contains(rec.Body.String(), want) {
			return fmt.Errorf("Body = %q wanted to contain %q", got, want)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		t.Error(err)
	}
}

func TestHandlerBreakerTimeout(t *testing.T) {
	// This test sends a request which will take a long time to complete.
	// Then another one with a very short context timeout.
	// Verifies that the second one fails with timeout.
	resp := make(chan struct{})
	blockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-resp
	})
	breaker := NewBreaker(BreakerParams{
		QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1,
	})
	stats := network.NewRequestStats(time.Now())

	h := ProxyHandler(breaker, stats, false /*tracingEnabled*/, blockHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:8081/time", nil)
	if err != nil {
		t.Fatal("NewRequestWithContext =", err)
	}
	var eg errgroup.Group
	barrier := make(chan struct{})
	eg.Go(func() error {
		// This will block.
		close(barrier)
		h(httptest.NewRecorder(), req)
		return nil
	})

	// For proper checks we need to ensure the order of requests.
	<-barrier
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:8081/time", nil)
	if err != nil {
		t.Fatal("NewRequestWithContext =", err)
	}
	eg.Go(func() error {
		defer close(resp) // Make the other request terminate.
		rec := httptest.NewRecorder()
		h(rec, req2)
		if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
			return fmt.Errorf("Code = %d, want: %d", got, want)
		}
		const want = "context deadline exceeded"
		if got := rec.Body.String(); !strings.Contains(rec.Body.String(), want) {
			return fmt.Errorf("Body = %q wanted to contain %q", got, want)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		t.Error(err)
	}
}

func TestHandlerReqEvent(t *testing.T) {
	params := BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	breaker := NewBreaker(params)
	for _, br := range []*Breaker{breaker, nil} {
		t.Run(fmt.Sprint("Breaker?=", br == nil), func(t *testing.T) {
			// This has to be here to capture subtest.
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

			stats := network.NewRequestStats(time.Now())
			h := ProxyHandler(br, stats, true /*tracingEnabled*/, proxy)

			writer := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

			// Verify the Original host header processing.
			req.Host = "nimporte.pas"
			req.Header.Set(network.OriginalHostHeader, wantHost)

			req.Header.Set(network.ProxyHeaderName, activator.Name)
			h(writer, req)

			if got := stats.Report(time.Now()).ProxiedRequestCount; got != 1 {
				t.Errorf("ProxiedRequestCount = %v, want 1", got)
			}
		})
	}
}

func TestIgnoreProbe(t *testing.T) {
	// Verifies that probes don't queue.
	resp := make(chan struct{})
	c := atomic.NewInt32(0)
	// Ensure we can receive 3 requests with CC=1.
	go func() {
		to := time.After(3 * time.Second)
		tick := time.NewTicker(10 * time.Millisecond)
		defer func() { tick.Stop() }()
		for {
			select {
			case <-tick.C:
				if c.Load() == 3 {
					close(resp)
					return
				}
			case <-to:
				// No fatal'ing in goroutines.
				t.Error("Timed out waiting to see 3 probes")
				return
			}
		}
	}()

	var httpHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		c.Inc()
		<-resp
		if !network.IsKubeletProbe(r) {
			t.Error("Request was not a probe")
			w.WriteHeader(http.StatusBadRequest)
		}
	}

	server := httptest.NewServer(httpHandler)
	serverURL, _ := url.Parse(server.URL)

	defer server.Close()
	proxy := httputil.NewSingleHostReverseProxy(serverURL)

	// Ensure no more than 1 request can be queued. So we'll send 3.
	breaker := NewBreaker(BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1})
	stats := network.NewRequestStats(time.Now())
	h := ProxyHandler(breaker, stats, false /*tracingEnabled*/, proxy)

	req := httptest.NewRequest(http.MethodPost, "http://prob.in", nil)
	req.Header.Set(network.KubeletProbeHeaderName, "1") // Mark it a probe.
	go h(httptest.NewRecorder(), req)
	go h(httptest.NewRecorder(), req)

	// Last one got synchronously.
	w := httptest.NewRecorder()
	h(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Errorf("Status got = %d, want: %d", got, want)
	}
}

func BenchmarkProxyHandler(b *testing.B) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	stats := network.NewRequestStats(time.Now())

	promStatReporter, err := NewPrometheusStatsReporter(
		"ns", "testksvc", "testksvc",
		"pod", reportingPeriod)
	if err != nil {
		b.Fatal("Failed to create stats reporter:", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(network.OriginalHostHeader, wantHost)

	tests := []struct {
		label        string
		breaker      *Breaker
		reportPeriod time.Duration
	}{{
		label:        "breaker-10-no-reports",
		breaker:      NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		reportPeriod: time.Hour,
	}, {
		label:        "breaker-infinite-no-reports",
		breaker:      nil,
		reportPeriod: time.Hour,
	}, {
		label:        "breaker-10-many-reports",
		breaker:      NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		reportPeriod: time.Microsecond,
	}, {
		label:        "breaker-infinite-many-reports",
		breaker:      nil,
		reportPeriod: time.Microsecond,
	}}

	for _, tc := range tests {
		reportTicker := time.NewTicker(tc.reportPeriod)

		go func() {
			for now := range reportTicker.C {
				promStatReporter.Report(stats.Report(now))
			}
		}()

		h := ProxyHandler(tc.breaker, stats, true /*tracingEnabled*/, baseHandler)
		b.Run(fmt.Sprintf("sequential-%s", tc.label), func(b *testing.B) {
			resp := httptest.NewRecorder()
			for j := 0; j < b.N; j++ {
				h(resp, req)
			}
		})
		b.Run(fmt.Sprintf("parallel-%s", tc.label), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				resp := httptest.NewRecorder()
				for pb.Next() {
					h(resp, req)
				}
			})
		})

		reportTicker.Stop()
	}
}
