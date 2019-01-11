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
	"context"
	"net/http"

	zipkinhttp "github.com/openzipkin/zipkin-go/middleware/http"
	"go.uber.org/zap"
)

type TracerRefGetter func(context.Context) (*TracerRef, error)

type spanHandler struct {
	opName   string
	next     http.Handler
	TRGetter TracerRefGetter
	logger   *zap.SugaredLogger
}

func (h *spanHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tr, err := h.TRGetter(r.Context())
	if err != nil {
		h.logger.Error("Failed to get tracer: %v", err)
		h.next.ServeHTTP(w, r)
		return
	}
	defer tr.Done()
	middleware := zipkinhttp.NewServerMiddleware(tr.Tracer, zipkinhttp.SpanName(h.opName))
	middleware(h.next).ServeHTTP(w, r)
}

// HTTPSpanMiddleware is a http.Handler middleware which creats a span and injects a ZipkinTracer in to the request context
func HTTPSpanMiddleware(logger *zap.SugaredLogger, opName string, trGetter TracerRefGetter, next http.Handler) http.Handler {
	return &spanHandler{
		opName:   opName,
		next:     next,
		TRGetter: trGetter,
		logger:   logger,
	}
}
