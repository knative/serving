/*
Copyright 2019 The Knative Authors.

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

package v1alpha1

import (
	"testing"

	apitest "knative.dev/pkg/apis/testing"
)

func TestMetricTypicalFlow(t *testing.T) {
	m := &MetricStatus{}
	m.InitializeConditions()
	apitest.CheckConditionOngoing(m.duck(), MetricConditionActive, t)
	apitest.CheckConditionOngoing(m.duck(), MetricConditionReady, t)

	// When we see successful scraping, we mark the Metric active.
	m.MarkActive()
	apitest.CheckConditionSucceeded(m.duck(), MetricConditionActive, t)
	apitest.CheckConditionSucceeded(m.duck(), MetricConditionReady, t)

	// Check idempotency.
	m.MarkActive()
	apitest.CheckConditionSucceeded(m.duck(), MetricConditionActive, t)
	apitest.CheckConditionSucceeded(m.duck(), MetricConditionReady, t)

	// When we stop seeing successful scrapes, we mark the Metric inactive.
	m.MarkInactive("TheReason", "the message")
	apitest.CheckConditionFailed(m.duck(), MetricConditionActive, t)
	apitest.CheckConditionFailed(m.duck(), MetricConditionReady, t)

	// Eventually, we can see successful scrapes again.
	m.MarkActive()
	apitest.CheckConditionSucceeded(m.duck(), MetricConditionActive, t)
	apitest.CheckConditionSucceeded(m.duck(), MetricConditionReady, t)
}
