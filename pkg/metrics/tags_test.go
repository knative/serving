/*
Copyright 2020 The Knative Authors

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

package metrics

import (
	"strings"
	"testing"

	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

var testM = stats.Int64(
	"test_metric",
	"A metric just for tests",
	stats.UnitDimensionless)

func register() {
	if err := view.Register(
		&view.View{
			Description: "Number of pods autoscaler wants to allocate",
			Measure:     testM,
			Aggregation: view.LastValue(),
			TagKeys:     CommonRevisionKeys,
		}); err != nil {
		panic(err)
	}
}

func TestNewStatsReporterCtxErrors(t *testing.T) {
	// These are invalid as defined by the current OpenCensus library.
	invalidTagValues := []string{
		"naïve",                  // Includes non-ASCII character.
		strings.Repeat("a", 256), // Longer than 255 characters.
	}

	for _, v := range invalidTagValues {
		if _, err := RevisionContext(v, v, v, v); err == nil {
			t.Errorf("Expected err to not be nil for value %q, got nil", v)
		}
	}
}

// Resets global state from the opencensus package
// Required to run at the beginning of tests that check metrics' values
// to make the tests idempotent.
func resetMetrics() {
	metricstest.Unregister(testM.Name())
	register()
}

func TestReporterEmptyServiceName(t *testing.T) {
	resetMetrics()
	// Metrics reported to an empty service name will be recorded with service "unknown" (metricskey.ValueUnknown).
	rctx, err := RevisionContext("testns", "" /*service=*/, "testconfig", "testrev")
	if err != nil {
		t.Fatalf("Failed to create a new context: %v", err)
	}
	wantTags := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       metricskey.ValueUnknown,
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
	}
	pkgmetrics.Record(rctx, testM.M(42))
	metricstest.CheckLastValueData(t, "test_metric", wantTags, 42)
}
