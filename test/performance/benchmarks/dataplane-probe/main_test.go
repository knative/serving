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
