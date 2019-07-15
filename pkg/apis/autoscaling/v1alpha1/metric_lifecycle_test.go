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
