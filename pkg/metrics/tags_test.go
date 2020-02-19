/*
Copyright 2020 The Knative Authors

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

package metrics

import (
	"context"
	"strings"
	"testing"

	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

var testM = stats.Int64(
	"test_metric",
	"A metric just for tests",
	stats.UnitDimensionless)

func register(t *testing.T) func() {
	if err := view.Register(
		&view.View{
			Description: "Number of pods autoscaler wants to allocate",
			Measure:     testM,
			Aggregation: view.LastValue(),
			TagKeys:     append(CommonRevisionKeys, ResponseCodeKey, ResponseCodeClassKey, PodTagKey, ContainerTagKey),
		}); err != nil {
		t.Fatalf("Failed to register view: %v", err)
	}

	return func() {
		metricstest.Unregister(testM.Name())
	}
}

func TestContextsErrors(t *testing.T) {
	// These are invalid as defined by the current OpenCensus library.
	invalidTagValues := []string{
		"naïve",                  // Includes non-ASCII character.
		strings.Repeat("a", 256), // Longer than 255 characters.
	}
	for _, v := range invalidTagValues {
		if _, err := PodContext(v, v); err == nil {
			t.Errorf("PodContext(%q) = nil, wanted an error", v)
		}
		if _, err := RevisionContext(v, v, v, v); err == nil {
			t.Errorf("RevisionContext(%q) = nil, wanted an error", v)
		}
		if _, err := PodRevisionContext(v, v, v, v, v, v); err == nil {
			t.Errorf("PodRevisionContext(%q) = nil, wanted an error", v)
		}
		if _, err := AugmentWithRevision(context.Background(), v, v, v, v); err == nil {
			t.Errorf("AugmentWithRevision(%q) = nil, wanted an error", v)
		}
	}
}

func TestContexts(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		wantTags map[string]string
	}{{
		name: "pod context",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodContext("testpod", "testcontainer")
		}),
		wantTags: map[string]string{
			"pod_name":       "testpod",
			"container_name": "testcontainer",
		},
	}, {
		name: "revision context",
		ctx: mustCtx(t, func() (context.Context, error) {
			return RevisionContext("testns", "testsvc", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			metricskey.LabelNamespaceName:     "testns",
			metricskey.LabelServiceName:       "testsvc",
			metricskey.LabelConfigurationName: "testcfg",
			metricskey.LabelRevisionName:      "testrev",
		},
	}, {
		name: "revision context (empty svc)",
		ctx: mustCtx(t, func() (context.Context, error) {
			return RevisionContext("testns", "", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			metricskey.LabelNamespaceName:     "testns",
			metricskey.LabelServiceName:       metricskey.ValueUnknown,
			metricskey.LabelConfigurationName: "testcfg",
			metricskey.LabelRevisionName:      "testrev",
		},
	}, {
		name: "pod revision context",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodRevisionContext("testpod", "testcontainer", "testns", "testsvc", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			"pod_name":                        "testpod",
			"container_name":                  "testcontainer",
			metricskey.LabelNamespaceName:     "testns",
			metricskey.LabelServiceName:       "testsvc",
			metricskey.LabelConfigurationName: "testcfg",
			metricskey.LabelRevisionName:      "testrev",
		},
	}, {
		name: "pod revision context (empty svc)",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodRevisionContext("testpod", "testcontainer", "testns", "", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			"pod_name":                        "testpod",
			"container_name":                  "testcontainer",
			metricskey.LabelNamespaceName:     "testns",
			metricskey.LabelServiceName:       metricskey.ValueUnknown,
			metricskey.LabelConfigurationName: "testcfg",
			metricskey.LabelRevisionName:      "testrev",
		},
	}, {
		name: "pod revision context (empty svc)",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodRevisionContext("testpod", "testcontainer", "testns", "", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			"pod_name":                        "testpod",
			"container_name":                  "testcontainer",
			metricskey.LabelNamespaceName:     "testns",
			metricskey.LabelServiceName:       metricskey.ValueUnknown,
			metricskey.LabelConfigurationName: "testcfg",
			metricskey.LabelRevisionName:      "testrev",
		},
	}, {
		name: "pod context augmented with revision",
		ctx: mustCtx(t, func() (context.Context, error) {
			ctx, err := PodContext("testpod", "testcontainer")
			if err != nil {
				return ctx, err
			}
			return AugmentWithRevision(ctx, "testns", "testsvc", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			"pod_name":                        "testpod",
			"container_name":                  "testcontainer",
			metricskey.LabelNamespaceName:     "testns",
			metricskey.LabelServiceName:       "testsvc",
			metricskey.LabelConfigurationName: "testcfg",
			metricskey.LabelRevisionName:      "testrev",
		},
	}, {
		name: "pod revision context augmented with response",
		ctx: mustCtx(t, func() (context.Context, error) {
			ctx, err := PodRevisionContext("testpod", "testcontainer", "testns", "", "testcfg", "testrev")
			return AugmentWithResponse(ctx, 200), err
		}),
		wantTags: map[string]string{
			"pod_name":                        "testpod",
			"container_name":                  "testcontainer",
			metricskey.LabelNamespaceName:     "testns",
			metricskey.LabelServiceName:       metricskey.ValueUnknown,
			metricskey.LabelConfigurationName: "testcfg",
			metricskey.LabelRevisionName:      "testrev",
			metricskey.LabelResponseCode:      "200",
			metricskey.LabelResponseCodeClass: "2xx",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cancel := register(t)
			defer cancel()

			pkgmetrics.Record(test.ctx, testM.M(42))
			metricstest.CheckLastValueData(t, "test_metric", test.wantTags, 42)
		})
	}
}

func mustCtx(t *testing.T, f func() (context.Context, error)) context.Context {
	t.Helper()

	// Force a way around the cache.
	contextCache.Purge()

	ctx, err := f()
	if err != nil {
		t.Fatalf("Failed to create a new context: %v", err)
	}
	return ctx
}
