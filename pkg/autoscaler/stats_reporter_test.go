/*
Copyright 2018 The Knative Authors

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

package autoscaler

import (
	"strings"
	"testing"

	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"
)

func TestNewStatsReporterErrors(t *testing.T) {
	// These are invalid as defined by the current OpenCensus library.
	invalidTagValues := []string{
		"naïve",                  // Includes non-ASCII character.
		strings.Repeat("a", 256), // Longer than 255 characters.
	}

	for _, v := range invalidTagValues {
		_, err := NewStatsReporter(v, v, v, v)
		if err == nil {
			t.Errorf("Expected err to not be nil for value %q, got nil", v)
		}
	}
}

func TestReporterReport(t *testing.T) {
	resetMetrics()

	r, err := NewStatsReporter("testns", "testsvc", "testconfig", "testrev")
	if err != nil {
		t.Fatalf("Failed to create a new reporter: %v", err)
	}
	wantTags := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       "testsvc",
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
	}

	// Send statistics only once and observe the results
	r.ReportDesiredPodCount(10)
	r.ReportPanic(0)
	r.ReportRequestConcurrency(2, 3, 0.9)
	r.ReportRPS(5, 6, 7)
	r.ReportExcessBurstCapacity(19.84)
	metricstest.CheckLastValueData(t, "desired_pods", wantTags, 10)
	metricstest.CheckLastValueData(t, "panic_mode", wantTags, 0)
	metricstest.CheckLastValueData(t, "stable_request_concurrency", wantTags, 2)
	metricstest.CheckLastValueData(t, "excess_burst_capacity", wantTags, 19.84)
	metricstest.CheckLastValueData(t, "panic_request_concurrency", wantTags, 3)
	metricstest.CheckLastValueData(t, "target_concurrency_per_pod", wantTags, 0.9)
	metricstest.CheckLastValueData(t, "stable_requests_per_second", wantTags, 5)
	metricstest.CheckLastValueData(t, "panic_requests_per_second", wantTags, 6)
	metricstest.CheckLastValueData(t, "target_requests_per_second", wantTags, 7)

	// All the stats are gauges - record multiple entries for one stat - last one should stick
	r.ReportDesiredPodCount(1)
	r.ReportDesiredPodCount(2)
	r.ReportDesiredPodCount(3)
	metricstest.CheckLastValueData(t, "desired_pods", wantTags, 3)

	r.ReportPanic(1)
	r.ReportPanic(0)
	r.ReportPanic(1)
	metricstest.CheckLastValueData(t, "panic_mode", wantTags, 1)

	r.ReportPanic(0)
	metricstest.CheckLastValueData(t, "panic_mode", wantTags, 0)
}

func TestReporterEmptyServiceName(t *testing.T) {
	resetMetrics()
	// Metrics reported to an empty service name will be recorded with service "unknown" (metricskey.ValueUnknown).
	r, err := NewStatsReporter("testns", "" /*service=*/, "testconfig", "testrev")
	if err != nil {
		t.Fatalf("Failed to create a new reporter: %v", err)
	}
	wantTags := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       metricskey.ValueUnknown,
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
	}
	r.ReportDesiredPodCount(10)
	metricstest.CheckLastValueData(t, "desired_pods", wantTags, 10)
}

// Resets global state from the opencensus package
// Required to run at the beginning of tests that check metrics' values
// to make the tests idempotent.
func resetMetrics() {
	metricstest.Unregister(
		desiredPodCountM.Name(),
		stableRequestConcurrencyM.Name(),
		panicRequestConcurrencyM.Name(),
		excessBurstCapacityM.Name(),
		targetRequestConcurrencyM.Name(),
		panicM.Name(),
		stableRPSM.Name(),
		panicRPSM.Name(),
		targetRPSM.Name())
	register()
}
