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
	"time"

	"github.com/golang/protobuf/proto"
	tpb "github.com/google/mako/clients/proto/analyzers/threshold_analyzer_go_proto"
	mpb "github.com/google/mako/spec/proto/mako_go_proto"
	vegeta "github.com/tsenart/vegeta/lib"
)

var (
	// This analyzer validates that the p95 latency talking to pods through a Kubernetes
	// Service falls in the +5ms range.  This does not have Knative or Istio components
	// on the dataplane, and so it is intended as a canary to flag environmental
	// problems that might be causing contemporaneous Knative or Istio runs to fall out of SLA.
	Kubernetes95PercentileLatency = &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Kubernetes baseline"),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(100 * time.Millisecond),
			Max: bound(105 * time.Millisecond),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: proto.Int32(95000),
				ValueKey:            proto.String("kd"),
			},
		}},
	}

	// This analyzer validates that the p95 latency talking to pods through Istio
	// falls in the +8ms range.  This does not actually have Knative components
	// on the dataplane, and so it is intended as a canary to flag environmental
	// problems that might be causing contemporaneous Knative runs to fall out of SLA.
	Istio95PercentileLatency = &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Istio baseline"),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(100 * time.Millisecond),
			Max: bound(108 * time.Millisecond),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: proto.Int32(95000),
				ValueKey:            proto.String("id"),
			},
		}},
	}

	// This analyzer validates that the p95 latency hitting a Knative Service
	// going through JUST the queue-proxy falls in the +10ms range.
	Queue95PercentileLatency = &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Queue p95 latency"),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(100 * time.Millisecond),
			Max: bound(110 * time.Millisecond),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: proto.Int32(95000),
				ValueKey:            proto.String("qp"),
			},
		}},
	}

	// This analyzer validates that the p95 latency hitting a Knative Service
	// going through BOTH the activator and queue-proxy falls in the +10ms range.
	Activator95PercentileLatency = &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Activator p95 latency"),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(100 * time.Millisecond),
			Max: bound(110 * time.Millisecond),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: proto.Int32(95000),
				ValueKey:            proto.String("a"),
			},
		}},
	}

	// Map the above to our benchmark targets.
	targets = map[string]struct {
		target    vegeta.Target
		stat      string
		estat     string
		analyzers []*tpb.ThresholdAnalyzerInput
	}{
		"deployment": {
			target: vegeta.Target{
				Method: "GET",
				URL:    "http://deployment.default.svc.cluster.local?sleep=100",
			},
			stat:      "kd",
			estat:     "ke",
			analyzers: []*tpb.ThresholdAnalyzerInput{Kubernetes95PercentileLatency},
		},
		"istio": {
			target: vegeta.Target{
				Method: "GET",
				URL:    "http://istio.default.svc.cluster.local?sleep=100",
			},
			stat:      "id",
			estat:     "ie",
			analyzers: []*tpb.ThresholdAnalyzerInput{Istio95PercentileLatency},
		},
		"queue": {
			target: vegeta.Target{
				Method: "GET",
				URL:    "http://queue-proxy.default.svc.cluster.local?sleep=100",
			},
			stat:      "qp",
			estat:     "qe",
			analyzers: []*tpb.ThresholdAnalyzerInput{Queue95PercentileLatency},
		},
		"activator": {
			target: vegeta.Target{
				Method: "GET",
				URL:    "http://activator.default.svc.cluster.local?sleep=100",
			},
			stat:      "a",
			estat:     "ae",
			analyzers: []*tpb.ThresholdAnalyzerInput{Activator95PercentileLatency},
		},
	}
)

// bound is a helper for making the inline SLOs more readable by expressing
// them as durations.
func bound(d time.Duration) *float64 {
	return proto.Float64(d.Seconds())
}
