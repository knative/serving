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

	"github.com/knative/pkg/metrics/metricskey"
	"go.opencensus.io/stats/view"
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

func TestReporter_Report(t *testing.T) {
	r := &Reporter{}
	if err := r.ReportDesiredPodCount(10); err == nil {
		t.Error("Reporter.ReportDesiredPodCount() expected an error for Report call before init. Got success.")
	}

	r, _ = NewStatsReporter("testns", "testsvc", "testconfig", "testrev")
	wantTags := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       "testsvc",
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
	}

	// Send statistics only once and observe the results
	expectSuccess(t, "ReportDesiredPodCount", func() error { return r.ReportDesiredPodCount(10) })
	expectSuccess(t, "ReportRequestedPodCount", func() error { return r.ReportRequestedPodCount(7) })
	expectSuccess(t, "ReportActualPodCount", func() error { return r.ReportActualPodCount(5) })
	expectSuccess(t, "ReportPanic", func() error { return r.ReportPanic(0) })
	expectSuccess(t, "ReportObservedPodCount", func() error { return r.ReportObservedPodCount(1) })
	expectSuccess(t, "ReportStableRequestConcurrency", func() error { return r.ReportStableRequestConcurrency(2) })
	expectSuccess(t, "ReportPanicRequestConcurrency", func() error { return r.ReportPanicRequestConcurrency(3) })
	expectSuccess(t, "ReportTargetRequestConcurrency", func() error { return r.ReportTargetRequestConcurrency(0.9) })
	checkData(t, "desired_pods", wantTags, 10)
	checkData(t, "requested_pods", wantTags, 7)
	checkData(t, "actual_pods", wantTags, 5)
	checkData(t, "panic_mode", wantTags, 0)
	checkData(t, "observed_pods", wantTags, 1)
	checkData(t, "stable_request_concurrency", wantTags, 2)
	checkData(t, "panic_request_concurrency", wantTags, 3)
	checkData(t, "target_concurrency_per_pod", wantTags, 0.9)

	// All the stats are gauges - record multiple entries for one stat - last one should stick
	expectSuccess(t, "ReportDesiredPodCount", func() error { return r.ReportDesiredPodCount(1) })
	expectSuccess(t, "ReportDesiredPodCount", func() error { return r.ReportDesiredPodCount(2) })
	expectSuccess(t, "ReportDesiredPodCount", func() error { return r.ReportDesiredPodCount(3) })
	checkData(t, "desired_pods", wantTags, 3)

	expectSuccess(t, "ReportRequestedPodCount", func() error { return r.ReportRequestedPodCount(4) })
	expectSuccess(t, "ReportRequestedPodCount", func() error { return r.ReportRequestedPodCount(5) })
	expectSuccess(t, "ReportRequestedPodCount", func() error { return r.ReportRequestedPodCount(6) })
	checkData(t, "requested_pods", wantTags, 6)

	expectSuccess(t, "ReportActualPodCount", func() error { return r.ReportActualPodCount(7) })
	expectSuccess(t, "ReportActualPodCount", func() error { return r.ReportActualPodCount(8) })
	expectSuccess(t, "ReportActualPodCount", func() error { return r.ReportActualPodCount(9) })
	checkData(t, "actual_pods", wantTags, 9)

	expectSuccess(t, "ReportPanic", func() error { return r.ReportPanic(1) })
	expectSuccess(t, "ReportPanic", func() error { return r.ReportPanic(0) })
	expectSuccess(t, "ReportPanic", func() error { return r.ReportPanic(1) })
	checkData(t, "panic_mode", wantTags, 1)

	expectSuccess(t, "ReportPanic", func() error { return r.ReportPanic(0) })
	checkData(t, "panic_mode", wantTags, 0)
}

func expectSuccess(t *testing.T, funcName string, f func() error) {
	if err := f(); err != nil {
		t.Errorf("Reporter.%v() expected success but got error %v", funcName, err)
	}
}

func checkData(t *testing.T, name string, wantTags map[string]string, wantValue float64) {
	if d, err := view.RetrieveData(name); err != nil {
		t.Errorf("Got unexpected error from view.RetrieveData error: %v", err)
	} else {
		if len(d) != 1 {
			t.Errorf("Want 1 data row but got %d from view.RetrieveData", len(d))
		}
		for _, got := range d[0].Tags {
			if want, ok := wantTags[got.Key.Name()]; !ok {
				t.Errorf("Got an extra tag from view.RetrieveData: (%v, %v)", got.Key.Name(), got.Value)
			} else {
				if got.Value != want {
					t.Errorf("Expected a different tag value from view.RetrieveData for key:%v. Got=%v, want=%v", got.Key.Name(), got.Value, want)
				}
			}
		}

		if s, ok := d[0].Data.(*view.LastValueData); !ok {
			t.Error("Expected a LastValueData type from view.RetrieveData")
		} else {
			if s.Value != wantValue {
				t.Errorf("Expected value=%v for metric %v from view.RetrieveData, but got=%v", wantValue, name, s.Value)
			}
		}
	}
}
