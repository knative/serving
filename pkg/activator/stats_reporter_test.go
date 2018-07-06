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

	"go.opencensus.io/stats/view"
)

// var expectedType = map[string]struct{}{
// 	"LastValueData": view.LastValueData,
// 	"CountData":     view.CountData,
// }

func TestActivatorReporter_Report(t *testing.T) {
	r := &Reporter{}

	if err := r.Report("testNs", "testConfig", "testRev", RequestCountReserveM, 1); err == nil {
		t.Error("Reporter.Report() expected an error for Report call before init. Got success.")
	}

	r, _ = NewStatsReporter()
	wantTags := map[string]string{
		"configuration_namespace": "testns",
		"configuration":           "testconfig",
		"revision":                "testrev",
	}
	expectSuccess(t, func() error { return r.Report("testns", "testconfig", "testrev", RequestCountReserveM, 1) })
	expectSuccess(t, func() error { return r.Report("testns", "testconfig", "testrev", RequestCountReserveM, 1) })
	checkData(t, "request_count_reserve", wantTags, 2)
}

func expectSuccess(t *testing.T, f func() error) {
	if err := f(); err != nil {
		t.Errorf("Reporter.Report() expected success but got error %v", err)
	}
}

func checkData(t *testing.T, name string, wantTags map[string]string, wantValue int) {
	if d, err := view.RetrieveData(name); err != nil {
		t.Errorf("Reporter.Report() error = %v, wantErr %v", err, false)
	} else {
		if len(d) != 1 {
			t.Errorf("Reporter.Report() len(d) %v, want %v", len(d), 1)
		}
		for _, got := range d[0].Tags {
			if want, ok := wantTags[got.Key.Name()]; !ok {
				t.Errorf("Reporter.Report() got an extra tag %v: %v", got.Key.Name(), got.Value)
			} else {
				if got.Value != want {
					t.Errorf("Reporter.Report() expected a different tag value. key:%v, got: %v, want: %v", got.Key.Name(), got.Value, want)
				}
			}
		}

		if s, ok := d[0].Data.(*view.CountData); !ok {
			t.Error("Reporter.Report() expected a CountData type")
		} else {
			if s.Value != (int64)(wantValue) {
				t.Errorf("Reporter.Report() expected %v got %v. metric: %v", (int64)(wantValue), s.Value, name)
			}
		}
	}
}
