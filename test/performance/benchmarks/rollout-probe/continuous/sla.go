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

package main

import (
	"net/http"
	"time"

	tpb "github.com/google/mako/clients/proto/analyzers/threshold_analyzer_go_proto"
	mpb "github.com/google/mako/spec/proto/mako_go_proto"
	vegeta "github.com/tsenart/vegeta/lib"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/mako"
)

// This function constructs an analyzer that validates the p95 aggregate value of the given metric.
func new95PercentileLatency(name, valueKey string, min, max time.Duration) *tpb.ThresholdAnalyzerInput {
	return &tpb.ThresholdAnalyzerInput{
		Name: ptr.String(name),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(min),
			Max: bound(max),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: ptr.Int32(95000),
				ValueKey:            ptr.String(valueKey),
			},
		}},
		CrossRunConfig: mako.NewCrossRunConfig(10),
	}
}

// This analyzer validates that the p95 latency hitting a Knative Service
// going through JUST the queue-proxy falls in the +10ms range.
func newQueue95PercentileLatency(valueKey string) *tpb.ThresholdAnalyzerInput {
	return new95PercentileLatency("Queue p95 latency", valueKey, 100*time.Millisecond, 110*time.Millisecond)
}

// This analyzer validates that the p95 latency hitting a Knative Service
// going through BOTH the activator and queue-proxy falls in the +10ms range.
func newActivator95PercentileLatency(valueKey string) *tpb.ThresholdAnalyzerInput {
	return new95PercentileLatency("Activator p95 latency", valueKey, 100*time.Millisecond, 110*time.Millisecond)
}

var (
	// Map the above to our benchmark targets.
	targets = map[string]struct {
		target    vegeta.Target
		stat      string
		estat     string
		analyzers []*tpb.ThresholdAnalyzerInput
	}{
		"queue-proxy": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy.default.svc?sleep=100",
			},
			stat:      "q",
			estat:     "qe",
			analyzers: []*tpb.ThresholdAnalyzerInput{newQueue95PercentileLatency("q")},
		},
		"queue-proxy-with-cc": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy-with-cc.default.svc?sleep=100",
			},
			stat:  "qc",
			estat: "qce",
			// We use the same threshold analyzer, since we want Breaker to exert minimal latency impact.
			analyzers: []*tpb.ThresholdAnalyzerInput{newQueue95PercentileLatency("qc")},
		},
		"activator": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator.default.svc?sleep=100",
			},
			stat:      "a",
			estat:     "ae",
			analyzers: []*tpb.ThresholdAnalyzerInput{newActivator95PercentileLatency("a")},
		},
		"activator-with-cc": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator-with-cc.default.svc?sleep=100",
			},
			stat:  "ac",
			estat: "ace",
			// We use the same threshold analyzer, since we want Throttler/Breaker to exert minimal latency impact.
			analyzers: []*tpb.ThresholdAnalyzerInput{newActivator95PercentileLatency("ac")},
		},
	}
)

// bound is a helper for making the inline SLOs more readable by expressing
// them as durations.
func bound(d time.Duration) *float64 {
	return ptr.Float64(d.Seconds())
}
