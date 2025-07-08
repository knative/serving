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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

func NewTracingHandler(tp trace.TracerProvider, next http.Handler) http.Handler {
	shim := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		defer func() {
			// otelhttp middleware creates the labeler
			labeler, _ := otelhttp.LabelerFromContext(r.Context())
			span := trace.SpanFromContext(r.Context())

			// otelhttp doesn't add labeler attributes to the span
			span.SetAttributes(labeler.Get()...)
		}()

		next.ServeHTTP(rw, r)
	})

	return otelhttp.NewHandler(shim, "activate",
		otelhttp.WithTracerProvider(tp),
	)
}
