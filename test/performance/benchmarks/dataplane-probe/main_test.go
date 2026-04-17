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

func TestCheckSLATotalRequestsTolerance(t *testing.T) {
	rate := vegeta.Rate{Freq: 1000, Per: time.Second}

	tests := []struct {
		name     string
		requests uint64
		wantErr  bool
	}{
		{name: "within tolerance below expected", requests: 999},
		{name: "within tolerance above expected", requests: 1001},
		{name: "outside tolerance below expected", requests: 998, wantErr: true},
		{name: "outside tolerance above expected", requests: 1002, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := &vegeta.Metrics{
				Requests: tt.requests,
				Latencies: vegeta.LatencyMetrics{
					P95: 10 * time.Millisecond,
				},
			}

			err := checkSLA(results, 0, 20*time.Millisecond, rate, time.Second)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkSLA() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
