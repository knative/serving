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
