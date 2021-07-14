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
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/metrics/metricstest"
	_ "knative.dev/pkg/metrics/testing"

	"go.opencensus.io/resource"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var testM = stats.Int64(
	"test_metric",
	"A metric just for tests",
	stats.UnitDimensionless)

func register(t *testing.T) func() {
	if err := pkgmetrics.RegisterResourceView(
		&view.View{
			Description: "Number of pods autoscaler wants to allocate",
			Measure:     testM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{ResponseCodeKey, ResponseCodeClassKey, PodKey, ContainerKey},
		}); err != nil {
		t.Fatal("Failed to register view:", err)
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
		if _, err := PodRevisionContext(v, v, v, v, v, v); err == nil {
			t.Errorf("PodRevisionContext(%q) = nil, wanted an error", v)
		}
	}
}

func TestContexts(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		wantTags     map[string]string
		wantResource *resource.Resource
	}{{
		name: "pod context",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodContext("testpod", "testcontainer")
		}),
		wantTags: map[string]string{
			LabelPodName:       "testpod",
			LabelContainerName: "testcontainer",
		},
	}, {
		name: "revision context",
		ctx: purge(t, func() context.Context {
			return RevisionContext("testns", "testsvc", "testcfg", "testrev")
		}),
		wantTags: map[string]string{},
		wantResource: &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				LabelNamespaceName:     "testns",
				LabelServiceName:       "testsvc",
				LabelConfigurationName: "testcfg",
				LabelRevisionName:      "testrev",
			},
		},
	}, {
		name: "revision context (empty svc)",
		ctx: purge(t, func() context.Context {
			return RevisionContext("testns", "", "testcfg", "testrev")
		}),
		wantTags: map[string]string{},
		wantResource: &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				LabelNamespaceName:     "testns",
				LabelServiceName:       ValueUnknown,
				LabelConfigurationName: "testcfg",
				LabelRevisionName:      "testrev",
			},
		},
	}, {
		name: "pod revision context",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodRevisionContext("testpod", "testcontainer", "testns", "testsvc", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			LabelPodName:       "testpod",
			LabelContainerName: "testcontainer",
		},
		wantResource: &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				LabelNamespaceName:     "testns",
				LabelServiceName:       "testsvc",
				LabelConfigurationName: "testcfg",
				LabelRevisionName:      "testrev",
			},
		},
	}, {
		name: "pod revision context (empty svc)",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodRevisionContext("testpod", "testcontainer", "testns", "", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			LabelPodName:       "testpod",
			LabelContainerName: "testcontainer",
		},
		wantResource: &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				LabelNamespaceName:     "testns",
				LabelServiceName:       ValueUnknown,
				LabelConfigurationName: "testcfg",
				LabelRevisionName:      "testrev",
			},
		},
	}, {
		name: "pod revision context (empty svc)",
		ctx: mustCtx(t, func() (context.Context, error) {
			return PodRevisionContext("testpod", "testcontainer", "testns", "", "testcfg", "testrev")
		}),
		wantTags: map[string]string{
			LabelPodName:       "testpod",
			LabelContainerName: "testcontainer",
		},
		wantResource: &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				LabelNamespaceName:     "testns",
				LabelServiceName:       ValueUnknown,
				LabelConfigurationName: "testcfg",
				LabelRevisionName:      "testrev",
			},
		},
	}, {
		name: "pod context augmented with revision",
		ctx: mustCtx(t, func() (context.Context, error) {
			ctx, err := PodContext("testpod", "testcontainer")
			if err != nil {
				return ctx, err
			}
			return AugmentWithRevision(ctx, "testns", "testsvc", "testcfg", "testrev"), nil
		}),
		wantTags: map[string]string{
			LabelPodName:       "testpod",
			LabelContainerName: "testcontainer",
		},
		wantResource: &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				LabelNamespaceName:     "testns",
				LabelServiceName:       "testsvc",
				LabelConfigurationName: "testcfg",
				LabelRevisionName:      "testrev",
			},
		},
	}, {
		name: "pod revision context augmented with response",
		ctx: mustCtx(t, func() (context.Context, error) {
			ctx, err := PodRevisionContext("testpod", "testcontainer", "testns", "testsvc", "testcfg", "testrev")
			return AugmentWithResponse(ctx, 200), err
		}),
		wantTags: map[string]string{
			LabelPodName:           "testpod",
			LabelContainerName:     "testcontainer",
			LabelResponseCode:      "200",
			LabelResponseCodeClass: "2xx",
		},
		wantResource: &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				LabelNamespaceName:     "testns",
				LabelServiceName:       "testsvc",
				LabelConfigurationName: "testcfg",
				LabelRevisionName:      "testrev",
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cancel := register(t)
			defer cancel()

			pkgmetrics.Record(test.ctx, testM.M(42))
			metricstest.AssertMetric(t, metricstest.IntMetric("test_metric", 42, test.wantTags).WithResource(test.wantResource))
		})
	}
}

func BenchmarkPodRevisionContext(b *testing.B) {
	// test with 1 (always hits cache),  1024 (25% load), 4095 (always hits cache, but at capacity),
	// 16k (often misses the cache) and 409600  (practically always misses cache)
	for _, revisions := range []int{1, 1024, 4095, 0xFFFF, 409600} {
		b.Run(fmt.Sprintf("sequential-%d-revisions", revisions), func(b *testing.B) {
			contextCache.Purge()
			for i := 0; i < b.N; i++ {
				rev := "name" + strconv.Itoa(rand.Intn(revisions))
				PodRevisionContext("pod", "container", "ns", "svc", "cfg", rev)
			}
		})

		b.Run(fmt.Sprintf("parallel-%d-revisions", revisions), func(b *testing.B) {
			contextCache.Purge()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					rev := "name" + strconv.Itoa(rand.Intn(revisions))
					PodRevisionContext("pod", "container", "ns", "svc", "cfg", rev)
				}
			})
		})
	}
}

func mustCtx(t *testing.T, f func() (context.Context, error)) context.Context {
	t.Helper()

	// Force a way around the cache.
	contextCache.Purge()

	ctx, err := f()
	if err != nil {
		t.Fatal("Failed to create a new context:", err)
	}
	return ctx
}

func purge(t *testing.T, f func() context.Context) context.Context {
	t.Helper()

	// Force a way around the cache.
	contextCache.Purge()

	ctx := f()
	return ctx
}
