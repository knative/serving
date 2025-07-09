/*
Copyright 2025 The Knative Authors

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

package queue

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/serving/pkg/metrics"
)

type routeTagHandler struct {
	next http.Handler
}

// NewRouteTagHandler annotates the request metrics and span with the route tag
func NewRouteTagHandler(next http.Handler) http.Handler {
	return &routeTagHandler{next: next}
}

func (h *routeTagHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tag := GetRouteTagNameFromRequest(r)

	// otelhttp sets the labeler
	labeler, _ := otelhttp.LabelerFromContext(r.Context())
	labeler.Add(metrics.RouteTagNameKey.With(tag))

	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(metrics.RouteTagNameKey.With(tag))

	h.next.ServeHTTP(w, r)
}

const (
	defaultTagName   = "kn:default"
	undefinedTagName = "kn:undefined"
	disabledTagName  = "kn:disabled"
)

// GetRouteTagNameFromRequest extracts the value of the tag header from http.Request
func GetRouteTagNameFromRequest(r *http.Request) string {
	name := r.Header.Get(netheader.RouteTagKey)
	isDefaultRoute := r.Header.Get(netheader.DefaultRouteKey)

	if name == "" {
		if isDefaultRoute == "" {
			// If there are no tag header and no `Knative-Serving-Default-Route` header,
			// it means that the tag header based routing is disabled, so the tag value is set to `disabled`.
			return disabledTagName
		}
		// If there is no tag header, just returns "default".
		return defaultTagName
	} else if isDefaultRoute == "true" {
		// If there is a tag header with not-empty string and the request is routed via the default route,
		// returns "undefined".
		return undefinedTagName
	}
	// Otherwise, returns the value of the tag header.
	return name
}
