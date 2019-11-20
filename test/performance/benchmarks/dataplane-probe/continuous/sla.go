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
	"net/http"
	"time"

	"github.com/golang/protobuf/proto"
	tpb "github.com/google/mako/clients/proto/analyzers/threshold_analyzer_go_proto"
	mpb "github.com/google/mako/spec/proto/mako_go_proto"
	vegeta "github.com/tsenart/vegeta/lib"
	"knative.dev/pkg/test/mako"
)

// This function constructs an analyzer that validates the p95 aggregate value of the given metric.
func new95PercentileLatency(name, valueKey string, min, max time.Duration) *tpb.ThresholdAnalyzerInput {
	return &tpb.ThresholdAnalyzerInput{
		Name: proto.String(name),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(min),
			Max: bound(max),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: proto.Int32(95000),
				ValueKey:            proto.String(valueKey),
			},
		}},
		CrossRunConfig: mako.NewCrossRunConfig(10),
	}
}

// This analyzer validates that the p95 latency talking to pods through a Kubernetes
// Service falls in the +5ms range.  This does not have Knative or Istio components
// on the dataplane, and so it is intended as a canary to flag environmental
// problems that might be causing contemporaneous Knative or Istio runs to fall out of SLA.
func newKubernetes95PercentileLatency(valueKey string) *tpb.ThresholdAnalyzerInput {
	return new95PercentileLatency("Kubernetes baseline", valueKey, 100*time.Millisecond, 105*time.Millisecond)
}

// This analyzer validates that the p95 latency talking to pods through Istio
// falls in the +8ms range.  This does not actually have Knative components
// on the dataplane, and so it is intended as a canary to flag environmental
// problems that might be causing contemporaneous Knative runs to fall out of SLA.
func newIstio95PercentileLatency(valueKey string) *tpb.ThresholdAnalyzerInput {
	return new95PercentileLatency("Istio baseline", valueKey, 100*time.Millisecond, 108*time.Millisecond)
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
		"deployment": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://deployment.default.svc.cluster.local?sleep=100",
			},
			stat:      "kd",
			estat:     "ke",
			analyzers: []*tpb.ThresholdAnalyzerInput{newKubernetes95PercentileLatency("kd")},
		},
		"istio": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://istio.default.svc.cluster.local?sleep=100",
			},
			stat:      "id",
			estat:     "ie",
			analyzers: []*tpb.ThresholdAnalyzerInput{newIstio95PercentileLatency("id")},
		},
		"queue": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy.default.svc.cluster.local?sleep=100",
			},
			stat:      "qp",
			estat:     "qe",
			analyzers: []*tpb.ThresholdAnalyzerInput{newQueue95PercentileLatency("qp")},
		},
		"queue-with-cc": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy-with-cc.default.svc.cluster.local?sleep=100",
			},
			stat:  "qc",
			estat: "re",
			// We use the same threshold analyzer, since we want Breaker to exert minimal latency impact.
			analyzers: []*tpb.ThresholdAnalyzerInput{newQueue95PercentileLatency("qc")},
		},
		"queue-with-cc-10": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy-with-cc-10.default.svc.cluster.local?sleep=100",
			},
			stat:  "qct",
			estat: "ret",
			// TODO(vagababov): determine values here.
			analyzers: []*tpb.ThresholdAnalyzerInput{},
		},
		"queue-with-cc-1": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy-with-cc-1.default.svc.cluster.local?sleep=100",
			},
			stat:  "qc1",
			estat: "re1",
			// TODO(vagababov): determine values here.
			analyzers: []*tpb.ThresholdAnalyzerInput{},
		},
		"activator": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator.default.svc.cluster.local?sleep=100",
			},
			stat:      "a",
			estat:     "ae",
			analyzers: []*tpb.ThresholdAnalyzerInput{newActivator95PercentileLatency("a")},
		},
		"activator-with-cc": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator-with-cc.default.svc.cluster.local?sleep=100",
			},
			stat:  "ac",
			estat: "be",
			// We use the same threshold analyzer, since we want Throttler/Breaker to exert minimal latency impact.
			analyzers: []*tpb.ThresholdAnalyzerInput{newActivator95PercentileLatency("ac")},
		},
		"activator-with-cc-10": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator-with-cc-10.default.svc.cluster.local?sleep=100",
			},
			stat:  "act",
			estat: "bet",
			// TODO(vagababov): determine values here.
			analyzers: []*tpb.ThresholdAnalyzerInput{},
		},
		"activator-with-cc-1": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator-with-cc-1.default.svc.cluster.local?sleep=100",
			},
			stat:  "ac1",
			estat: "be1",
			// TODO(vagababov): determine values here.
			analyzers: []*tpb.ThresholdAnalyzerInput{},
		},
	}
)

// bound is a helper for making the inline SLOs more readable by expressing
// them as durations.
func bound(d time.Duration) *float64 {
	return proto.Float64(d.Seconds())
}
