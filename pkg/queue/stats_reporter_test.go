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

package queue

import (
	"errors"
	"testing"

	"go.opencensus.io/stats/view"
)

const (
	namespace = "default"
	config    = "helloworld-go"
	revision  = "helloworld-go-00001"
)

func TestNewStatsReporter_negative(t *testing.T) {
	tests := []struct {
		name      string
		errorMsg  string
		result    error
		namespace string
		config    string
		revision  string
	}{
		{
			"Empty_Namespace_Value",
			"Expected namespace empty error",
			errors.New("Namespace must not be empty"),
			"",
			config,
			revision,
		},
		{
			"Empty_Config_Value",
			"Expected config empty error",
			errors.New("Config must not be empty"),
			namespace,
			"",
			revision,
		},
		{
			"Empty_Revision_Value",
			"Expected revision empty error",
			errors.New("Revision must not be empty"),
			namespace,
			config,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewStatsReporter(test.namespace, test.config, test.revision); err.Error() != test.result.Error() {
				t.Errorf("%+v, got: '%+v'", test.errorMsg, err)
			}
		})
	}
}

func TestNewStatsReporter_doubledeclare(t *testing.T) {
	reporter, err := NewStatsReporter(namespace, config, revision)
	if err != nil {
		t.Error("Something went wrong with creating a reporter.")
	}
	if _, err := NewStatsReporter(namespace, config, revision); err == nil {
		t.Error("Something went wrong with double declaration of reporter.")
	}
	reporter.UnregisterViews()
	if reporter.Initialized {
		t.Error("Reporter should not be initialized")
	}
}

func TestReporter_Report(t *testing.T) {
	reporter, err := NewStatsReporter(namespace, config, revision)
	if err != nil {
		t.Errorf("Something went wrong with creating a reporter, '%v'.", err)
	}
	if err := reporter.Report(true, float64(39), float64(3)); err != nil {
		t.Error(err)
	}
	checkData(t, LameDuckN, 0)
	checkData(t, OperationsPerSecondN, 39)
	checkData(t, AverageConcurrentRequestsN, 3)
	if err := reporter.UnregisterViews(); err != nil {
		t.Errorf("Error with unregistering views, %v", err)
	}
	if reporter.Initialized {
		t.Error("Reporter should not be initialized")
	}
	if err := reporter.UnregisterViews(); err == nil {
		t.Errorf("Error with unregistering views, %v", err)
	}
}

func checkData(t *testing.T, measurementName string, wanted float64) {
	if v, err := view.RetrieveData(measurementName); err != nil {
		t.Errorf("Reporter.Report() error = %v", err)
	} else {
		if got := v[0].Data.(*view.LastValueData); wanted != got.Value {
			t.Errorf("Wanted %v, Got %v", wanted, got.Value)
		}
	}
}
