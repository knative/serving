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
	"strconv"

	lru "github.com/hashicorp/golang-lru"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/metrics/metricskey"

	"go.opencensus.io/resource"
	"go.opencensus.io/tag"
)

var (
	// contextCache stores the metrics recorder contexts
	// in an LRU cache.
	// Hashicorp LRU cache is synchronized.
	contextCache *lru.Cache
)

// This is a fairly arbitrary number but we want it to be higher than the
// number of active revisions a single activator might be handling, to avoid
// churning the cache, without being so much higher that it causes an
// (effective) memory leak.
// The contents of the cache are quite small, so we can err on the high side.
const lruCacheSize = 4096

func init() {
	// The only possible error is when cache size is not positive.
	contextCache, _ = lru.New(lruCacheSize)
}

func valueOrUnknown(v string) string {
	if v != "" {
		return v
	}
	return metricskey.ValueUnknown
}

// RevisionContext generates a new base metric reporting context containing
// the respective revision specific tags.
func RevisionContext(ns, svc, cfg, rev string) context.Context {
	key := types.NamespacedName{Namespace: ns, Name: rev}
	ctx, ok := contextCache.Get(key)
	if !ok {
		rctx := AugmentWithRevision(context.Background(), ns, svc, cfg, rev)
		contextCache.Add(key, rctx)
		ctx = rctx
	}
	return ctx.(context.Context)
}

type podCtx struct {
	pod, container string
}

// PodContext generate a new base metric reporting context containing
// the respective pod specific tags.
func PodContext(pod, container string) (context.Context, error) {
	key := podCtx{pod: pod, container: container}
	ctx, ok := contextCache.Get(key)
	if !ok {
		rctx, err := tag.New(
			context.Background(),
			tag.Upsert(PodTagKey, pod),
			tag.Upsert(ContainerTagKey, container))
		if err != nil {
			return rctx, err
		}
		contextCache.Add(key, rctx)
		ctx = rctx
	}
	return ctx.(context.Context), nil
}

type podRevisionCtx struct {
	pod      podCtx
	revision types.NamespacedName
}

// PodRevisionContext generates a new base metric reporting context containing
// the respective pod and revision specific tags.
func PodRevisionContext(pod, container, ns, svc, cfg, rev string) (context.Context, error) {
	key := podRevisionCtx{
		pod:      podCtx{pod: pod, container: container},
		revision: types.NamespacedName{Namespace: ns, Name: rev},
	}
	ctx, ok := contextCache.Get(key)
	if !ok {
		rctx, err := PodContext(pod, container)
		if err != nil {
			return rctx, err
		}
		rctx = AugmentWithRevision(rctx, ns, svc, cfg, rev)
		contextCache.Add(key, rctx)
		ctx = rctx
	}
	return ctx.(context.Context), nil
}

// AugmentWithRevision augments the given context with a knative_revision resource.
func AugmentWithRevision(baseCtx context.Context, ns, svc, cfg, rev string) context.Context {
	r := resource.Resource{
		Type: metricskey.ResourceTypeKnativeRevision,
		Labels: map[string]string{
			metricskey.LabelNamespaceName:     ns,
			metricskey.LabelServiceName:       valueOrUnknown(svc),
			metricskey.LabelConfigurationName: cfg,
			metricskey.LabelRevisionName:      rev,
		},
	}
	return metricskey.WithResource(baseCtx, r)
}

// AugmentWithResponse augments the given context with response-code specific tags.
func AugmentWithResponse(baseCtx context.Context, responseCode int) context.Context {
	ctx, _ := tag.New(
		baseCtx,
		tag.Upsert(ResponseCodeKey, strconv.Itoa(responseCode)),
		tag.Upsert(ResponseCodeClassKey, responseCodeClass(responseCode)))
	return ctx
}

// AugmentWithResponseAndRouteTag augments the given context with response-code and route-tag specific tags.
func AugmentWithResponseAndRouteTag(baseCtx context.Context, responseCode int, routeTag string) context.Context {
	ctx, _ := tag.New(
		baseCtx,
		tag.Upsert(ResponseCodeKey, strconv.Itoa(responseCode)),
		tag.Upsert(ResponseCodeClassKey, responseCodeClass(responseCode)),
		tag.Upsert(RouteTagKey, routeTag))
	return ctx
}

// responseCodeClass converts response code to a string of response code class.
// e.g. The response code class is "5xx" for response code 503.
func responseCodeClass(responseCode int) string {
	// Get the hundreds digit of the response code and concatenate "xx".
	return strconv.Itoa(responseCode/100) + "xx"
}
