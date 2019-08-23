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
	// tpb "github.com/google/mako/clients/proto/analyzers/threshold_analyzer_go_proto"
	// mpb "github.com/google/mako/spec/proto/mako_go_proto"
)

var (
// TODO(mattmoor): bounds on latencies.
)

// bound is a helper for making the inline SLOs more readable by expressing
// them as durations.
func bound(d time.Duration) *float64 {
	return proto.Float64(d.Seconds())
}
