/*
Copyright 2021 The Knative Authors

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
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	pkglogging "knative.dev/pkg/logging"

	network "knative.dev/networking/pkg"
)

func TestConcurrencyStateHandler(t *testing.T) {
	tests := []struct {
		name            string
		pauses, resumes int64
		events          map[time.Duration]time.Duration // start time => req length
	}{{
		name:    "single request",
		pauses:  1,
		resumes: 1,
		events: map[time.Duration]time.Duration{
			1 * time.Second: 2 * time.Second,
		},
	}, {
		name:    "overlapping requests",
		pauses:  1,
		resumes: 1,
		events: map[time.Duration]time.Duration{
			25 * time.Millisecond: 100 * time.Millisecond,
			75 * time.Millisecond: 200 * time.Millisecond,
		},
	}, {
		name:    "subsumbed request",
		pauses:  1,
		resumes: 1,
		events: map[time.Duration]time.Duration{
			25 * time.Millisecond: 300 * time.Millisecond,
			75 * time.Millisecond: 200 * time.Millisecond,
		},
	}, {
		name:    "start stop start",
		pauses:  2,
		resumes: 2,
		events: map[time.Duration]time.Duration{
			25 * time.Millisecond:  300 * time.Millisecond,
			75 * time.Millisecond:  200 * time.Millisecond,
			850 * time.Millisecond: 300 * time.Millisecond,
			900 * time.Millisecond: 400 * time.Millisecond,
		},
	}}

	logger, _ := pkglogging.NewLogger("", "error")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paused := atomic.NewInt64(0)
			pause := func() {
				paused.Inc()
			}

			resumed := atomic.NewInt64(0)
			resume := func() {
				resumed.Inc()
			}

			delegated := atomic.NewInt64(0)
			delegate := func(w http.ResponseWriter, r *http.Request) {
				wait, err := strconv.Atoi(r.Header.Get("wait"))
				if err != nil {
					panic(err)
				}

				time.Sleep(time.Duration(wait))
				delegated.Inc()
			}

			h := ConcurrencyStateHandler(logger, http.HandlerFunc(delegate), pause, resume)

			var wg sync.WaitGroup
			wg.Add(len(tt.events))
			for delay, length := range tt.events {
				length := length
				time.AfterFunc(delay, func() {
					w := httptest.NewRecorder()
					r := httptest.NewRequest("GET", "http://target", nil)
					r.Header.Set("wait", strconv.FormatInt(int64(length), 10))
					h.ServeHTTP(w, r)
					wg.Done()
				})
			}

			wg.Wait()
			// Allow last update to finish (otherwise values are off, though this doesn't show
			// as a race condition when running `go test -race `
			// TODO Less hacky fix for this
			time.Sleep(100 * time.Microsecond)

			if got, want := paused.Load(), tt.pauses; got != want {
				t.Errorf("expected to be paused %d times, but was paused %d times", want, got)
			}

			if got, want := delegated.Load(), int64(len(tt.events)); got != want {
				t.Errorf("expected to be delegated %d times, but delegated %d times", want, got)
			}

			if got, want := resumed.Load(), tt.resumes; got != want {
				t.Errorf("expected to be resumed %d times, but was resumed %d times", want, got)
			}
		})
	}
}

func BenchmarkConcurrencyStateProxyHandler(b *testing.B) {
	logger, _ := pkglogging.NewLogger("", "error")
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

		h := ConcurrencyStateHandler(logger, ProxyHandler(tc.breaker, stats, true /*tracingEnabled*/, baseHandler), nil, nil)
		b.Run("sequential-"+tc.label, func(b *testing.B) {
			resp := httptest.NewRecorder()
			for j := 0; j < b.N; j++ {
				h(resp, req)
			}
		})
		b.Run("parallel-"+tc.label, func(b *testing.B) {
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
