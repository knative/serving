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

type report struct {
	Concurrency, ProxiedConcurrency float64
	Count, ProxiedCount             float64
}

func TestRequestStats(t *testing.T) {
	tests := []struct {
		name   string
		events func(in, out, inP, outP, report func(int64))
		want   []report
	}{{
		name: "no requests",
		events: func(in, out, inP, outP, report func(int64)) {
			report(1000)
		},
		want: []report{{
			Concurrency: 0,
			Count:       0,
		}},
	}, {
		name: "1 request, entire time",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			out(1000)
			report(1000)
		},
		want: []report{{
			Concurrency: 1,
			Count:       1,
		}},
	}, {
		name: "1 request, half the time",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			out(3000)
			report(6000)
		},
		want: []report{{
			Concurrency: 0.5,
			Count:       1,
		}},
	}, {
		name: "very short request",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			out(1)
			report(1000)
		},
		want: []report{{
			Concurrency: float64(1) / float64(1000),
			Count:       1,
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
		want: []report{{
			Concurrency: 1,
			Count:       3,
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
		want: []report{{
			Concurrency: 1.5,
			Count:       2,
		}},
	}, {
		name: "request across reporting",
		events: func(in, out, inP, outP, report func(int64)) {
			in(0)
			report(1000)
			out(1500)
			report(2000)
		},
		want: []report{{
			Concurrency: 1,
			Count:       1,
		}, {
			Concurrency: 0.5,
			Count:       0,
		}},
	}, {
		name: "1 request, proxied, entire time",
		events: func(in, out, inP, outP, report func(int64)) {
			inP(0)
			outP(1000)
			report(1000)
		},
		want: []report{{
			Concurrency:        1,
			ProxiedConcurrency: 1,
			Count:              1,
			ProxiedCount:       1,
		}},
	}, {
		name: "1 request, proxied, half the time",
		events: func(in, out, inP, outP, report func(int64)) {
			inP(0)
			outP(500)
			report(1000)
		},
		want: []report{{
			Concurrency:        0.5,
			ProxiedConcurrency: 0.5,
			Count:              1,
			ProxiedCount:       1,
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
		want: []report{{
			Concurrency:        1.5,
			ProxiedConcurrency: 0.5,
			Count:              2,
			ProxiedCount:       1,
		}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// All tests are relative to epoch.
			stats := NewRequestStats(time.Unix(0, 0))
			reports := make([]report, 0, len(test.want))
			test.events(
				eventFunc(stats, ReqIn),
				eventFunc(stats, ReqOut),
				eventFunc(stats, ProxiedIn),
				eventFunc(stats, ProxiedOut),
				func(ms int64) {
					aC, aPC, rC, pC := stats.Report(time.Unix(0, ms*int64(time.Millisecond)))
					reports = append(reports, report{
						Concurrency:        aC,
						ProxiedConcurrency: aPC,
						Count:              rC,
						ProxiedCount:       pC,
					})
				},
			)

			if !cmp.Equal(reports, test.want) {
				t.Errorf("Got = %v, want = %v, diff %s", reports, test.want, cmp.Diff(reports, test.want))
			}
		})
	}
}

func eventFunc(stats *RequestStats, typ ReqEventType) func(int64) {
	return func(ms int64) {
		stats.HandleEvent(ReqEvent{
			Time: time.Unix(0, ms*int64(time.Millisecond)),
			Type: typ,
		})
	}
}
