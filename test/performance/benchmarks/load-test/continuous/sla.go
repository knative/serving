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
	"knative.dev/pkg/test/mako"
)

// This analyzer validates that the p95 latency over the 0->3k stepped burst
// falls in the +15ms range.   This includes a mix of cold-starts and steady
// state (once the autoscaling decisions have leveled off).
func newLoadTest95PercentileLatency(tags ...string) *tpb.ThresholdAnalyzerInput {
	return &tpb.ThresholdAnalyzerInput{
		Name: proto.String("95p latency"),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(100 * time.Millisecond),
			Max: bound(115 * time.Millisecond),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: proto.Int32(95000),
				ValueKey:            proto.String("l"),
			},
		}},
		CrossRunConfig: mako.NewCrossRunConfig(10, tags...),
	}
}

// This analyzer validates that the maximum request latency observed over the 0->3k
// stepped burst is no more than +10 seconds.  This is not strictly a cold-start
// metric, but it is a superset that includes steady state latency and the latency
// of non-cold-start overload requests.
func newLoadTestMaximumLatency(tags ...string) *tpb.ThresholdAnalyzerInput {
	return &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Maximum latency"),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(100 * time.Millisecond),
			Max: bound(100*time.Millisecond + 10*time.Second),
			DataFilter: &mpb.DataFilter{
				DataType: mpb.DataFilter_METRIC_AGGREGATE_MAX.Enum(),
				ValueKey: proto.String("l"),
			},
		}},
		CrossRunConfig: mako.NewCrossRunConfig(10, tags...),
	}
}

// This analyzer validates that the mean error rate observed over the 0->3k
// stepped burst is 0.
func newLoadTestMaximumErrorRate(tags ...string) *tpb.ThresholdAnalyzerInput {
	return &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Mean error rate"),
		Configs: []*tpb.ThresholdConfig{{
			Max: proto.Float64(0),
			DataFilter: &mpb.DataFilter{
				DataType: mpb.DataFilter_METRIC_AGGREGATE_MEAN.Enum(),
				ValueKey: proto.String("es"),
			},
		}},
		CrossRunConfig: mako.NewCrossRunConfig(10, tags...),
	}
}

// bound is a helper for making the inline SLOs more readable by expressing
// them as durations.
func bound(d time.Duration) *float64 {
	return proto.Float64(d.Seconds())
}
