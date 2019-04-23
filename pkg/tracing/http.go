/*
Copyright 2019 The Knative Authors

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

package tracing

import (
	"net/http"

	"go.opencensus.io/trace"
)

type spanHandler struct {
	opName string
	next   http.Handler
}

// ServeHTTP is an HTTP handler which injects a tracing span using a TracerRefGetter
func (h *spanHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, newSpan := trace.StartSpan(r.Context(), h.opName)
	defer newSpan.End()
	r = r.WithContext(ctx)
	h.next.ServeHTTP(w, r)
}

// HTTPSpanMiddleware is a http.Handler middleware which creats a span and injects a ZipkinTracer in to the request context
func HTTPSpanMiddleware(opName string, next http.Handler) http.Handler {
	return &spanHandler{
		opName: opName,
		next:   next,
	}
}
