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

package queue

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"
	netheader "knative.dev/networking/pkg/http/header"
	netstats "knative.dev/networking/pkg/http/stats"
	"knative.dev/serving/pkg/activator"
)

// ProxyHandler sends requests to the `next` handler at a rate controlled by
// the passed `breaker`, while recording stats to `stats`.
func ProxyHandler(
	tracer trace.Tracer,
	breaker *Breaker,
	stats *netstats.RequestStats,
	next http.Handler,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if netheader.IsKubeletProbe(r) {
			next.ServeHTTP(w, r)
			return
		}

		ctx, proxySpan := tracer.Start(r.Context(), "kn.queueproxy.proxy")
		r = r.WithContext(ctx)
		defer proxySpan.End()

		// Metrics for autoscaling.
		in, out := netstats.ReqIn, netstats.ReqOut
		if activator.Name == netheader.GetKnativeProxyValue(r) {
			in, out = netstats.ProxiedIn, netstats.ProxiedOut
		}
		stats.HandleEvent(netstats.ReqEvent{Time: time.Now(), Type: in})
		defer func() {
			stats.HandleEvent(netstats.ReqEvent{Time: time.Now(), Type: out})
		}()

		netheader.RewriteHostOut(r)

		if breaker == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Enforce queuing and concurrency limits.
		ctx, waitSpan := tracer.Start(r.Context(), "kn.queueproxy.wait")
		r = r.WithContext(ctx)

		if err := breaker.Maybe(r.Context(), func() {
			waitSpan.End()
			next.ServeHTTP(w, r)
		}); err != nil {
			waitSpan.End()
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRequestQueueFull) {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
			} else {
				// This line is most likely untestable :-).
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	}
}
