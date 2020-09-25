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
	"net/http"
	"time"

	"go.opencensus.io/trace"
	network "knative.dev/networking/pkg"
	"knative.dev/serving/pkg/activator"
)

// ProxyHandler sends requests to the `next` handler at a rate controlled by
// the passed `breaker`, while recording stats to `stats`.
func ProxyHandler(breaker *Breaker, stats *network.RequestStats, tracingEnabled bool, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if network.IsKubeletProbe(r) {
			next.ServeHTTP(w, r)
			return
		}

		if tracingEnabled {
			proxyCtx, proxySpan := trace.StartSpan(r.Context(), "queue_proxy")
			r = r.WithContext(proxyCtx)
			defer proxySpan.End()
		}

		// Metrics for autoscaling.
		in, out := network.ReqIn, network.ReqOut
		if activator.Name == network.KnativeProxyHeader(r) {
			in, out = network.ProxiedIn, network.ProxiedOut
		}
		stats.HandleEvent(network.ReqEvent{Time: time.Now(), Type: in})
		defer func() {
			stats.HandleEvent(network.ReqEvent{Time: time.Now(), Type: out})
		}()
		network.RewriteHostOut(r)

		// Enforce queuing and concurrency limits.
		if breaker != nil {
			var waitSpan *trace.Span
			if tracingEnabled {
				_, waitSpan = trace.StartSpan(r.Context(), "queue_wait")
			}
			if err := breaker.Maybe(r.Context(), func() {
				waitSpan.End()
				next.ServeHTTP(w, r)
			}); err != nil {
				waitSpan.End()
				switch err {
				case context.DeadlineExceeded, ErrRequestQueueFull:
					http.Error(w, err.Error(), http.StatusServiceUnavailable)
				default:
					// This line is most likely untestable :-).
					w.WriteHeader(http.StatusInternalServerError)
				}
			}
		} else {
			next.ServeHTTP(w, r)
		}
	}
}
