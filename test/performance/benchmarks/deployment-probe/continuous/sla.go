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

// This analyzer validates that the p95 latency deploying a new service takes up
// to 25 seconds.
func newDeploy95PercentileLatency(tags ...string) *tpb.ThresholdAnalyzerInput {
	return &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Deploy p95 latency"),
		Configs: []*tpb.ThresholdConfig{{
			Min: bound(0 * time.Second),
			Max: bound(25 * time.Second),
			DataFilter: &mpb.DataFilter{
				DataType:            mpb.DataFilter_METRIC_AGGREGATE_PERCENTILE.Enum(),
				PercentileMilliRank: proto.Int32(95000),
				ValueKey:            proto.String("dl"),
			},
		}},
		CrossRunConfig: mako.NewCrossRunConfig(10, tags...),
	}
}

// This analyzer validates that the number of services deployed to "Ready=True".
// Configured to run for 35m with a frequency of 5s, the theoretical limit is 420
// if deployments take 0s.  Factoring in deployment latency, we will miss a
// handful of the trailing deployments, so we relax this to 410.
func newReadyDeploymentCount(tags ...string) *tpb.ThresholdAnalyzerInput {
	return &tpb.ThresholdAnalyzerInput{
		Name: proto.String("Ready deployment count"),
		Configs: []*tpb.ThresholdConfig{{
			Min: proto.Float64(410),
			Max: proto.Float64(420),
			DataFilter: &mpb.DataFilter{
				DataType: mpb.DataFilter_METRIC_AGGREGATE_COUNT.Enum(),
				ValueKey: proto.String("dl"),
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
