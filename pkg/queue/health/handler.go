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
	"io"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/serving/pkg/queue"
)

const badProbeTemplate = "unexpected probe header value: "

// ProbeHandler returns a http.HandlerFunc that responds to health checks.
// This handler assumes the Knative Probe Header will be passed.
func ProbeHandler(tracer trace.Tracer, prober func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ph := netheader.GetKnativeProbeValue(r)

		if ph != queue.Name {
			http.Error(w, badProbeTemplate+ph, http.StatusBadRequest)
			return
		}

		if prober == nil {
			http.Error(w, "no probe", http.StatusInternalServerError)
			return
		}

		if !prober() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		io.WriteString(w, queue.Name)
	}
}
