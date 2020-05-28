/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package network

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestRequestStats(t *testing.T) {
	tests := []struct {
		name   string
		events func(in, out, inP, outP, report func(int64))
		want   []RequestStatsReport
	}{{
		name: "no requests",
		events: func(in, out, inP, outP, report func(int64)) {
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency: 0,
			RequestCount:       0,
		}},
	}, {
		name: "1 request, entire time",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			out(1000)
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency: 1,
			RequestCount:       1,
		}},
	}, {
		name: "1 request, half the time",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			out(3000)
			report(6000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency: 0.5,
			RequestCount:       1,
		}},
	}, {
		name: "very short request",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			out(1)
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency: float64(1) / float64(1000),
			RequestCount:       1,
		}},
	}, {
		name: "3 requests, fill entire time",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			out(300)
			in(300)
			out(600)
			in(600)
			out(1000)
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency: 1,
			RequestCount:       3,
		}},
	}, {
		name: "interleaved requests",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			in(100)
			out(600)
			out(1000)
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency: 1.5,
			RequestCount:       2,
		}},
	}, {
		name: "request across reporting",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			report(1000)
			out(1500)
			report(2000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency: 1,
			RequestCount:       1,
		}, {
			AverageConcurrency: 0.5,
			RequestCount:       0,
		}},
	}, {
		name: "1 request, proxied, entire time",
		events: func(in, out, inP, outP, report func(int64)) {
			inP(0)
			outP(1000)
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency:        1,
			AverageProxiedConcurrency: 1,
			RequestCount:              1,
			ProxiedRequestCount:       1,
		}},
	}, {
		name: "1 request, proxied, half the time",
		events: func(in, out, inP, outP, report func(int64)) {
			inP(0)
			outP(500)
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency:        0.5,
			AverageProxiedConcurrency: 0.5,
			RequestCount:              1,
			ProxiedRequestCount:       1,
		}},
	}, {
		name: "2 requests, proxied and non proxied",
		events: func(in, out, inP, outP, report func(int64)) {
			inP(0)
			in(0)
			outP(500)
			out(1000)
			report(1000)
		},
		want: []RequestStatsReport{{
			AverageConcurrency:        1.5,
			AverageProxiedConcurrency: 0.5,
			RequestCount:              2,
			ProxiedRequestCount:       1,
		}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// All tests are relative to epoch.
			stats := NewRequestStats(time.Time{})
			reports := make([]RequestStatsReport, 0, len(test.want))
			test.events(
				eventFunc(stats, ReqIn),
				eventFunc(stats, ReqOut),
				eventFunc(stats, ProxiedIn),
				eventFunc(stats, ProxiedOut),
				func(ms int64) {
					reports = append(reports, stats.Report(time.Time{}.Add(time.Duration(ms)*time.Millisecond)))
				},
			)

			if !cmp.Equal(reports, test.want) {
				t.Errorf("Got = %v, want = %v, diff(-want,+got): %s", reports, test.want, cmp.Diff(test.want, reports))
			}
		})
	}
}

func eventFunc(stats *RequestStats, typ ReqEventType) func(int64) {
	return func(ms int64) {
		stats.HandleEvent(ReqEvent{
			Time: time.Time{}.Add(time.Duration(ms) * time.Millisecond),
			Type: typ,
		})
	}
}

func BenchmarkRequestStatsDirect(b *testing.B) {
	stats := NewRequestStats(time.Now())

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		for {
			select {
			case <-ticker.C:
				stats.Report(time.Now())
			}
		}
	}()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			stats.HandleEvent(ReqEvent{
				Time: time.Now(),
				Type: ReqIn,
			})
			stats.HandleEvent(ReqEvent{
				Time: time.Now(),
				Type: ReqOut,
			})
		}
	})
}

func BenchmarkRequestStatsThroughChannels(b *testing.B) {
	reqChan := make(chan ReqEvent, 100)
	stats := NewRequestStats(time.Now())

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		for {
			select {
			case event := <-reqChan:
				stats.HandleEvent(event)
			case <-ticker.C:
				stats.Report(time.Now())
			}
		}
	}()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reqChan <- ReqEvent{
				Time: time.Time{},
				Type: ReqIn,
			}

			reqChan <- ReqEvent{
				Time: time.Time{},
				Type: ReqOut,
			}
		}
	})
}
