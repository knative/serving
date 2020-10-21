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

package health

import (
	"net/http"

	"go.opencensus.io/trace"

	network "knative.dev/networking/pkg"
	"knative.dev/serving/pkg/queue"
)

const badProbeTemplate = "unexpected probe header value: "

// ProbeHandler returns a http.HandlerFunc that responds to health checks if the
// knative network probe header is passed, and otherwise delegates to the next handler.
func ProbeHandler(healthState *State, prober func() bool, isAggressive bool, tracingEnabled bool, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ph := network.KnativeProbeHeader(r)

		if ph == "" {
			next.ServeHTTP(w, r)
			return
		}

		var probeSpan *trace.Span
		if tracingEnabled {
			_, probeSpan = trace.StartSpan(r.Context(), "probe")
			defer probeSpan.End()
		}

		if ph != queue.Name {
			http.Error(w, badProbeTemplate+ph, http.StatusBadRequest)
			probeSpan.Annotate([]trace.Attribute{
				trace.StringAttribute("queueproxy.probe.error", badProbeTemplate+ph)}, "error")
			return
		}

		if prober == nil {
			http.Error(w, "no probe", http.StatusInternalServerError)
			probeSpan.Annotate([]trace.Attribute{
				trace.StringAttribute("queueproxy.probe.error", "no probe")}, "error")
			return
		}

		healthState.HandleHealthProbe(func() bool {
			if !prober() {
				probeSpan.Annotate([]trace.Attribute{
					trace.StringAttribute("queueproxy.probe.error", "container not ready")}, "error")
				return false
			}
			return true
		}, isAggressive, w)
	}
}
