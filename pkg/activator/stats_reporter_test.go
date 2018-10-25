/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"testing"
	"time"

	"go.opencensus.io/stats/view"
)

func TestActivatorReporter(t *testing.T) {
	r := &Reporter{}

	if err := r.ReportRequest("testns", "testsvc", "testconfig", "testrev", 1); err == nil {
		t.Error("Reporter expected an error for Report call before init. Got success.")
	}
	if err := r.ReportResponseCount("testns", "testsvc", "testconfig", "testrev", 200, 1, 1); err == nil {
		t.Error("Reporter expected an error for Report call before init. Got success.")
	}

	var err error
	if r, err = NewStatsReporter(); err != nil {
		t.Error("Failed to create a new reporter.")
	}

	// test ReportRequest
	wantTags1 := map[string]string{
		"destination_namespace":     "testns",
		"destination_service":       "testsvc",
		"destination_configuration": "testconfig",
		"destination_revision":      "testrev",
		"serving_state":             "Reserved",
	}
	expectSuccess(t, func() error { return r.ReportRequest("testns", "testsvc", "testconfig", "testrev", 1) })
	expectSuccess(t, func() error { return r.ReportRequest("testns", "testsvc", "testconfig", "testrev", 2.0) })
	checkSumData(t, "revision_request_count", wantTags1, 3)

	// test ReportResponseCount
	wantTags2 := map[string]string{
		"destination_namespace":     "testns",
		"destination_service":       "testsvc",
		"destination_configuration": "testconfig",
		"destination_revision":      "testrev",
		"response_code":             "200",
		"num_tries":                 "6",
	}
	expectSuccess(t, func() error { return r.ReportResponseCount("testns", "testsvc", "testconfig", "testrev", 200, 6, 1) })
	expectSuccess(t, func() error { return r.ReportResponseCount("testns", "testsvc", "testconfig", "testrev", 200, 6, 3) })
	checkSumData(t, "revision_response_count", wantTags2, 4)

	// test ReportResponseTime
	wantTags3 := map[string]string{
		"destination_namespace":     "testns",
		"destination_service":       "testsvc",
		"destination_configuration": "testconfig",
		"destination_revision":      "testrev",
		"response_code":             "200",
	}
	expectSuccess(t, func() error {
		return r.ReportResponseTime("testns", "testsvc", "testconfig", "testrev", 200, 1100*time.Millisecond)
	})
	expectSuccess(t, func() error {
		return r.ReportResponseTime("testns", "testsvc", "testconfig", "testrev", 200, 9100*time.Millisecond)
	})
	checkDistributionData(t, "response_time_msec", wantTags3, 2, 1100, 9100)
}

func expectSuccess(t *testing.T, f func() error) {
	if err := f(); err != nil {
		t.Errorf("Reporter expected success but got error %v", err)
	}
}

func checkSumData(t *testing.T, name string, wantTags map[string]string, wantValue int) {
	if d, err := view.RetrieveData(name); err != nil {
		t.Errorf("Reporter error = %v, wantErr %v", err, false)
	} else {
		if len(d) != 1 {
			t.Errorf("Reporter len(d) %v, want %v", len(d), 1)
		}
		for _, got := range d[0].Tags {
			if want, ok := wantTags[got.Key.Name()]; !ok {
				t.Errorf("Reporter got an extra tag %v: %v", got.Key.Name(), got.Value)
			} else {
				if got.Value != want {
					t.Errorf("Reporter expected a different tag value. key:%v, got: %v, want: %v", got.Key.Name(), got.Value, want)
				}
			}
		}

		if s, ok := d[0].Data.(*view.SumData); !ok {
			t.Error("Reporter expected a SumData type")
		} else {
			if s.Value != (float64)(wantValue) {
				t.Errorf("Reporter expected %v got %v. metric: %v", (int64)(wantValue), s.Value, name)
			}
		}
	}
}

func checkDistributionData(t *testing.T, name string, wantTags map[string]string, expectedCount int, expectedMin float64, expectedMax float64) {
	if d, err := view.RetrieveData(name); err != nil {
		t.Errorf("Reporter error = %v, wantErr %v", err, false)
	} else {
		if len(d) != 1 {
			t.Errorf("Reporter len(d) %v, want %v", len(d), 1)
		}
		for _, got := range d[0].Tags {
			if want, ok := wantTags[got.Key.Name()]; !ok {
				t.Errorf("Reporter got an extra tag %v: %v", got.Key.Name(), got.Value)
			} else {
				if got.Value != want {
					t.Errorf("Reporter expected a different tag value. key:%v, got: %v, want: %v", got.Key.Name(), got.Value, want)
				}
			}
		}

		if s, ok := d[0].Data.(*view.DistributionData); !ok {
			t.Error("Reporter expected a DistributionData type")
		} else {
			if s.Count != int64(expectedCount) {
				t.Errorf("Reporter expected count %v got %v. metric: %v", (int64)(expectedCount), s.Count, name)
			}
			if s.Min != float64(expectedMin) {
				t.Errorf("Reporter expected min %v got %v. metric: %v", expectedMin, s.Min, name)
			}
			if s.Max != float64(expectedMax) {
				t.Errorf("Reporter expected max %v got %v. metric: %v", expectedMax, s.Max, name)
			}
		}
	}
}
