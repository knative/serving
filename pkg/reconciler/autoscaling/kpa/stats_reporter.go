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

package kpa

import (
	"context"

	lru "github.com/hashicorp/golang-lru"
	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/serving/pkg/metrics"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	requestedPodCountM = stats.Int64(
		"requested_pods",
		"Number of pods autoscaler requested from Kubernetes",
		stats.UnitDimensionless)
	actualPodCountM = stats.Int64(
		"actual_pods",
		"Number of pods that are allocated currently",
		stats.UnitDimensionless)
	notReadyPodCountM = stats.Int64(
		"not_ready_pods",
		"Number of pods that are not ready currently",
		stats.UnitDimensionless)
	pendingPodCountM = stats.Int64(
		"pending_pods",
		"Number of pods that are pending currently",
		stats.UnitDimensionless)
	terminatingPodCountM = stats.Int64(
		"terminating_pods",
		"Number of pods that are terminating currently",
		stats.UnitDimensionless)

	// recorderContextCache stores the metrics recorder contexts
	// in an LRU cache.
	// Hashicorp LRU cache is synchronized.
	recorderContextCache *lru.Cache
)

const lruCacheSize = 1024

func init() {
	register()
	// The only possible error is when cache size is not positive.
	lc, _ := lru.New(lruCacheSize)
	recorderContextCache = lc
}

func register() {
	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	if err := view.Register(
		&view.View{
			Description: "Number of pods autoscaler requested from Kubernetes",
			Measure:     requestedPodCountM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Number of pods that are allocated currently",
			Measure:     actualPodCountM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Number of pods that are not ready currently",
			Measure:     notReadyPodCountM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Number of pods that are pending currently",
			Measure:     pendingPodCountM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
		&view.View{
			Description: "Number of pods that are terminating currently",
			Measure:     terminatingPodCountM,
			Aggregation: view.LastValue(),
			TagKeys:     metrics.CommonRevisionKeys,
		},
	); err != nil {
		panic(err)
	}
}

func valueOrUnknown(v string) string {
	if v != "" {
		return v
	}
	return metricskey.ValueUnknown
}

func reporterContext(ns, service, config, revision string) (context.Context, error) {
	key := ns + "/" + revision
	ctx, ok := recorderContextCache.Get(key)
	if !ok {
		//  Note that service names can be an empty string, so they needs a special treatment.
		rctx, err := tag.New(
			context.Background(),
			tag.Upsert(metrics.NamespaceTagKey, ns),
			tag.Upsert(metrics.ServiceTagKey, valueOrUnknown(service)),
			tag.Upsert(metrics.ConfigTagKey, config),
			tag.Upsert(metrics.RevisionTagKey, revision))
		if err != nil {
			return nil, err
		}
		recorderContextCache.Add(key, rctx)
		ctx = rctx
	}
	return ctx.(context.Context), nil
}
