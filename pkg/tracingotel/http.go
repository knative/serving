/*
Copyright 2022 The Knative Authors

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

package tracingotel

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"k8s.io/apimachinery/pkg/util/sets"
)

// HTTPSpanMiddleware is an http.Handler middleware to create spans for the HTTP endpoint.
var HTTPSpanMiddleware = HTTPSpanIgnoringPaths()

// HTTPSpanIgnoringPaths is an http.Handler middleware to create spans for the HTTP
// endpoint, not sampling any request whose path is in pathsToIgnore.
func HTTPSpanIgnoringPaths(pathsToIgnore ...string) func(http.Handler) http.Handler {
	pathsToIgnoreSet := sets.NewString(pathsToIgnore...)
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(
			next,
			"server", // operation name, can be customized
			otelhttp.WithFilter(func(r *http.Request) bool {
				// If the path is in the ignore set, filter it (don't trace)
				return !pathsToIgnoreSet.Has(r.URL.Path)
			}),
			// otelhttp uses the global propagator by default.
			// otelhttp uses the global tracer provider by default.
		)
	}
}
