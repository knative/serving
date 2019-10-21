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

package activator

import (
	"net/http"
	"testing"
	"time"

	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"
)

// unregister, ehm, unregisters the metrics that were registered, by
// virtue of StatsReporter creation.
// Since golang executes test iterations within the same process, the stats reporter
// returns an error if the metric is already registered and the test panics.
func unregister() {
	metricstest.Unregister("request_count", "request_latencies", "request_concurrency")
}

func TestActivatorReporter(t *testing.T) {
	r := &Reporter{}

	if err := r.ReportRequestCount("testns", "testsvc", "testconfig", "testrev", http.StatusOK, 1); err == nil {
		t.Error("Reporter expected an error for Report call before init. Got success.")
	}

	var err error
	if r, err = NewStatsReporter("testpod"); err != nil {
		t.Fatalf("Failed to create a new reporter: %v", err)
	}
	// Without this `go test ... -count=X`, where X > 1, fails, since
	// we get an error about view already being registered.
	defer unregister()

	// test ReportResponseConcurrency
	wantTags1 := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       "testsvc",
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
		"pod_name":                        "testpod",
		"container_name":                  "activator",
	}
	expectSuccess(t, func() error {
		return r.ReportRequestConcurrency("testns", "testsvc", "testconfig", "testrev", 100)
	})
	metricstest.CheckLastValueData(t, "request_concurrency", wantTags1, 100)
	expectSuccess(t, func() error {
		return r.ReportRequestConcurrency("testns", "testsvc", "testconfig", "testrev", 200)
	})
	metricstest.CheckLastValueData(t, "request_concurrency", wantTags1, 200)

	// test ReportRequestCount
	wantTags2 := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       "testsvc",
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
		"pod_name":                        "testpod",
		"container_name":                  "activator",
		"response_code":                   "200",
		"response_code_class":             "2xx",
		"num_tries":                       "6",
	}
	expectSuccess(t, func() error {
		return r.ReportRequestCount("testns", "testsvc", "testconfig", "testrev", http.StatusOK, 6)
	})
	expectSuccess(t, func() error {
		return r.ReportRequestCount("testns", "testsvc", "testconfig", "testrev", http.StatusOK, 6)
	})
	metricstest.CheckCountData(t, "request_count", wantTags2, 2)

	// test ReportResponseTime
	wantTags3 := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       "testsvc",
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
		"pod_name":                        "testpod",
		"container_name":                  "activator",
		"response_code":                   "200",
		"response_code_class":             "2xx",
	}
	expectSuccess(t, func() error {
		return r.ReportResponseTime("testns", "testsvc", "testconfig", "testrev", http.StatusOK, 1100*time.Millisecond)
	})
	expectSuccess(t, func() error {
		return r.ReportResponseTime("testns", "testsvc", "testconfig", "testrev", http.StatusOK, 9100*time.Millisecond)
	})
	metricstest.CheckDistributionData(t, "request_latencies", wantTags3, 2, 1100.0, 9100.0)
}

func TestActivatorReporterEmptyServiceName(t *testing.T) {
	r, err := NewStatsReporter("testpod")
	defer unregister()

	if err != nil {
		t.Fatalf("Failed to create a new reporter: %v", err)
	}

	// test ReportResponseConcurrency
	wantTags1 := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       metricskey.ValueUnknown,
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
		"pod_name":                        "testpod",
		"container_name":                  "activator",
	}
	expectSuccess(t, func() error {
		return r.ReportRequestConcurrency("testns", "" /*service=*/, "testconfig", "testrev", 100)
	})
	metricstest.CheckLastValueData(t, "request_concurrency", wantTags1, 100)

	// test ReportRequestCount
	wantTags2 := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       metricskey.ValueUnknown,
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
		"pod_name":                        "testpod",
		"container_name":                  "activator",
		"response_code":                   "200",
		"response_code_class":             "2xx",
		"num_tries":                       "6",
	}
	expectSuccess(t, func() error {
		return r.ReportRequestCount("testns", "" /*service=*/, "testconfig", "testrev", 200, 6)
	})
	metricstest.CheckCountData(t, "request_count", wantTags2, 1)

	// test ReportResponseTime
	wantTags3 := map[string]string{
		metricskey.LabelNamespaceName:     "testns",
		metricskey.LabelServiceName:       metricskey.ValueUnknown,
		metricskey.LabelConfigurationName: "testconfig",
		metricskey.LabelRevisionName:      "testrev",
		"pod_name":                        "testpod",
		"container_name":                  "activator",
		"response_code":                   "200",
		"response_code_class":             "2xx",
	}
	expectSuccess(t, func() error {
		return r.ReportResponseTime("testns", "" /*service=*/, "testconfig", "testrev", 200, 7100*time.Millisecond)
	})
	expectSuccess(t, func() error {
		return r.ReportResponseTime("testns", "" /*service=*/, "testconfig", "testrev", 200, 5100*time.Millisecond)
	})
	metricstest.CheckDistributionData(t, "request_latencies", wantTags3, 2, 5100.0, 7100.0)
}

func expectSuccess(t *testing.T, f func() error) {
	t.Helper()
	if err := f(); err != nil {
		t.Errorf("Reporter expected success but got error: %v", err)
	}
}
