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
	"testing"
	"time"

	pkglogging "knative.dev/pkg/logging"

	network "knative.dev/networking/pkg"
)

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

		h := ConcurrencyStateHandler(logger, ProxyHandler(tc.breaker, stats, true /*tracingEnabled*/, baseHandler))
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
