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
	"go.opencensus.io/tag"
)

const (
	namespace = "default"
	config    = "helloworld-go"
	revision  = "helloworld-go-00001"
	pod       = "helloworld-go-00001-deployment-8ff587cc9-7g9gc"
)

func TestNewStatsReporter_negative(t *testing.T) {
	tests := []struct {
		name      string
		errorMsg  string
		result    error
		namespace string
		config    string
		revision  string
		pod       string
	}{
		{
			"Empty_Namespace_Value",
			"Expected namespace empty error",
			errors.New("Namespace must not be empty"),
			"",
			config,
			revision,
			pod,
		},
		{
			"Empty_Config_Value",
			"Expected config empty error",
			errors.New("Config must not be empty"),
			namespace,
			"",
			revision,
			pod,
		},
		{
			"Empty_Revision_Value",
			"Expected revision empty error",
			errors.New("Revision must not be empty"),
			namespace,
			config,
			"",
			pod,
		},
		{
			"Empty_Pod_Value",
			"Expected pod empty error",
			errors.New("Pod must not be empty"),
			namespace,
			config,
			revision,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewStatsReporter(test.namespace, test.config, test.revision, test.pod); err.Error() != test.result.Error() {
				t.Errorf("%+v, got: '%+v'", test.errorMsg, err)
			}
		})
	}
}

func TestNewStatsReporter_doubledeclare(t *testing.T) {
	reporter, err := NewStatsReporter(namespace, config, revision, pod)
	if err != nil {
		t.Error("Something went wrong with creating a reporter.")
	}
	if _, err := NewStatsReporter(namespace, config, revision, pod); err == nil {
		t.Error("Something went wrong with double declaration of reporter.")
	}
	reporter.UnregisterViews()
	if reporter.Initialized {
		t.Error("Reporter should not be initialized")
	}
}

func TestReporter_Report(t *testing.T) {
	testTagKeyValueMap, err := createTestTagKeyValueMap()
	if err != nil {
		t.Errorf("Something went wrong with creating tag, '%v'.", err)
	}
	reporter, err := NewStatsReporter(namespace, config, revision, pod)
	if err != nil {
		t.Errorf("Something went wrong with creating a reporter, '%v'.", err)
	}
	if err := reporter.Report(float64(39), float64(3)); err != nil {
		t.Error(err)
	}
	checkData(t, operationsPerSecondN, 39, testTagKeyValueMap)
	checkData(t, averageConcurrentRequestsN, 3, testTagKeyValueMap)
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

func checkData(t *testing.T, measurementName string, wanted float64, wantedTagKeyValueMap map[tag.Key]string) {
	if v, err := view.RetrieveData(measurementName); err != nil {
		t.Errorf("Reporter.Report() error = %v", err)
	} else {
		if got := v[0].Data.(*view.LastValueData); wanted != got.Value {
			t.Errorf("Wanted %v, Got %v", wanted, got.Value)
		}
		if len(v[0].Tags) != len(wantedTagKeyValueMap) {
			t.Errorf("Wanted %v, Got %v", wantedTagKeyValueMap, v[0].Tags)
		}
		for _, got := range v[0].Tags {
			if wanted, _ := wantedTagKeyValueMap[got.Key]; wanted != got.Value {
				t.Errorf("Wanted %v, Got %v", wanted, got.Value)
			}
		}
	}
}

func createTestTagKeyValueMap() (map[tag.Key]string, error) {
	nsTag, err := tag.NewKey("destination_namespace")
	if err != nil {
		return nil, err
	}
	configTag, err := tag.NewKey("destination_configuration")
	if err != nil {
		return nil, err
	}
	revTag, err := tag.NewKey("destination_revision")
	if err != nil {
		return nil, err
	}
	podTag, err := tag.NewKey("destination_pod")
	if err != nil {
		return nil, err
	}
	return map[tag.Key]string{
		nsTag:     namespace,
		configTag: config,
		revTag:    revision,
		podTag:    pod,
	}, nil
}
