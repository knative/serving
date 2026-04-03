/*
Copyright 2026 The Knative Authors

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
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestCheckSLAZeroParallelDoesNotUnderflow(t *testing.T) {
	results := &vegeta.Metrics{
		Requests: 0,
		Latencies: vegeta.LatencyMetrics{
			P95: 0,
			Max: 0,
		},
	}

	if err := checkSLA(results, 0, time.Millisecond, time.Millisecond, 0); err != nil {
		t.Fatalf("checkSLA() error = %v, want nil", err)
	}
}
