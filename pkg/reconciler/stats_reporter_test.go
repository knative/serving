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
	"context"
	"fmt"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"knative.dev/pkg/metrics/metricstest"
)

const (
	reconcilerMockName   = "mock_reconciler"
	testServiceNamespace = "test_namespace"
	testServiceName      = "test_service"
)

func TestNewStatsReporter(t *testing.T) {
	r, err := NewStatsReporter(reconcilerMockName)
	if err != nil {
		t.Errorf("Failed to create reporter: %v", err)
	}

	m := tag.FromContext(r.(*reporter).ctx)
	v, ok := m.Value(reconcilerTagKey)
	if !ok {
		t.Fatalf("Expected tag %q", reconcilerTagKey)
	}
	if v != reconcilerMockName {
		t.Fatalf("Expected %q for tag %q, got %q", reconcilerMockName, reconcilerTagKey, v)
	}
}

func TestReporter_ReportDuration(t *testing.T) {
	reporter, err := NewStatsReporter(reconcilerMockName)
	if err != nil {
		t.Errorf("Failed to create reporter: %v", err)
	}
	countWas := int64(0)
	if m := getMetric(t, ServiceReadyCountN); m != nil {
		countWas = m.Data.(*view.CountData).Value
	}

	if err = reporter.ReportServiceReady(testServiceNamespace, testServiceName, time.Second); err != nil {
		t.Error(err)
	}
	expectedTags := map[string]string{
		keyTagKey.Name():        fmt.Sprintf("%s/%s", testServiceNamespace, testServiceName),
		reconcilerTagKey.Name(): reconcilerMockName,
	}

	metricstest.CheckLastValueData(t, ServiceReadyLatencyN, expectedTags, 1000)
	metricstest.CheckCountData(t, ServiceReadyCountN, expectedTags, countWas+1)
}

func getMetric(t *testing.T, metric string) *view.Row {
	t.Helper()
	rows, err := view.RetrieveData(metric)
	if err != nil {
		t.Errorf("Failed retrieving data: %v", err)
	}
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func TestWithStatsReporter(t *testing.T) {
	if WithStatsReporter(context.Background(), nil) == nil {
		t.Errorf("stats reporter reports empty context")
	}
}
