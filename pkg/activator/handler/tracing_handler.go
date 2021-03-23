/*
Copyright 2021 The Knative Authors

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

package handler

import (
	"net/http"

	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	activatorconfig "knative.dev/serving/pkg/activator/config"
)

// NewTracingHandler creates a wrapper around tracing.HTTPSpanMiddleware that completely
// bypasses said handler when tracing is disabled via the Activator's configuration.
func NewTracingHandler(next http.Handler) http.HandlerFunc {
	tracingHandler := tracing.HTTPSpanMiddleware(next)
	return func(w http.ResponseWriter, r *http.Request) {
		tracingEnabled := activatorconfig.FromContext(r.Context()).Tracing.Backend != tracingconfig.None
		if !tracingEnabled {
			next.ServeHTTP(w, r)
			return
		}

		tracingHandler.ServeHTTP(w, r)
	}
}
