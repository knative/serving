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

package handler

import (
	"fmt"
	"knative.dev/serving/pkg/activator"
	"net/http"

	"knative.dev/serving/pkg/network"
)

// ProbeHandler handles responding to Knative internal network probes.
type ProbeHandler struct {
	NextHandler http.Handler
}

func (h *ProbeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	routeVersionHeader := "K-Route-Version"

	fmt.Printf("Request: %s, %s\n", r.Header.Get(network.ProbeHeaderName), r.Header.Get(routeVersionHeader))
	if val := r.Header.Get(network.ProbeHeaderName); val != "" {
		if val == "Ingress" { // TODO(bancel): use constant
			if val := r.Header.Get(routeVersionHeader); val != "" {
				w.Header().Set(routeVersionHeader, val)
				w.WriteHeader(200)
				fmt.Printf("return 200 with %s", val)
			} else {
				http.Error(w, fmt.Sprint("an Ingress probe request must contain a 'K-Route-Version' header"), http.StatusBadRequest)
			}
		} else if val != activator.Name {
			http.Error(w, fmt.Sprintf("unexpected probe header value: %q", val), http.StatusBadRequest)
		} else {
			w.Write([]byte(activator.Name))
			w.WriteHeader(200)
		}
	} else {
		r.Header.Del(routeVersionHeader)
		h.NextHandler.ServeHTTP(w, r)
	}
}
