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

package reconciler

import (
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"testing"
	"time"
)

const reconcilerMockName = "MockReconciler"

func TestReporter_ReportDuration(t *testing.T) {
	reporter := NewStatsReporter(reconcilerMockName)

	if err := reporter.ReportDuration(ServiceTimeUntilReadyM, time.Second); err != nil {
		t.Error(err)
	}

	rows, err := view.RetrieveData(ServiceTimeUntilReadyN)
	if err != nil {
		t.Errorf("Error retrieving data: %v", err)
	}

	if v := rows[0].Data.(*view.LastValueData).Value;  v != 1000 {
		t.Errorf("Wanted value %v, Got %v", 1000, v)
	}

	tags := rows[0].Tags
	if len(tags) != 1 {
		t.Errorf("Expected %v tags, got %v", 1, len(tags))
	}
	expectedTag := tag.Tag{Key: reconcilerTagKey, Value: reconcilerMockName}
	if tags[0] != expectedTag {
		t.Errorf("Expected tag %v, got %v", expectedTag, tags[0])
	}
}
