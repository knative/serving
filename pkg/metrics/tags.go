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
	"knative.dev/pkg/metrics/metricskey"

	"go.opencensus.io/tag"
)

var (
	// contextCache stores the metrics recorder contexts
	// in an LRU cache.
	// Hashicorp LRU cache is synchronized.
	contextCache *lru.Cache
)

const lruCacheSize = 1024

func init() {
	// The only possible error is when cache size is not positive.
	lc, _ := lru.New(lruCacheSize)
	contextCache = lc
}

func valueOrUnknown(v string) string {
	if v != "" {
		return v
	}
	return metricskey.ValueUnknown
}

// RevisionContext generates a new base metric reporting context containing
// the respective revision specific tags.
func RevisionContext(ns, service, config, revision string) (context.Context, error) {
	return AugmentWithRevision(context.Background(), ns, service, config, revision)
}

// AugmentWithRevision augments the given context with revision specific tags.
// Note: The passed in context will be cached as part of the new context. Do not
// use this function if the tags of the underlying contexts are non-static.
func AugmentWithRevision(baseCtx context.Context, ns, service, config, revision string) (context.Context, error) {
	key := ns + "/" + revision
	ctx, ok := contextCache.Get(key)
	if !ok {
		//  Note that service names can be an empty string, so they needs a special treatment.
		rctx, err := tag.New(
			baseCtx,
			tag.Upsert(NamespaceTagKey, ns),
			tag.Upsert(ServiceTagKey, valueOrUnknown(service)),
			tag.Upsert(ConfigTagKey, config),
			tag.Upsert(RevisionTagKey, revision))
		if err != nil {
			return nil, err
		}
		contextCache.Add(key, rctx)
		ctx = rctx
	}
	return ctx.(context.Context), nil
}

// AugmentWithResponse augments the given context with response-code specific tags.
func AugmentWithResponse(baseCtx context.Context, responseCode int) context.Context {
	ctx, err := tag.New(
		baseCtx,
		tag.Upsert(ResponseCodeKey, strconv.Itoa(responseCode)),
		tag.Upsert(ResponseCodeClassKey, responseCodeClass(responseCode)))
	if err != nil {
		// This should never happen but swallow the error regardless.
		return baseCtx
	}
	return ctx
}

// responseCodeClass converts response code to a string of response code class.
// e.g. The response code class is "5xx" for response code 503.
func responseCodeClass(responseCode int) string {
	// Get the hundred digit of the response code and concatenate "xx".
	return strconv.Itoa(responseCode/100) + "xx"
}
