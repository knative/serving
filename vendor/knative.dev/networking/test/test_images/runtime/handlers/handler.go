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

package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"

	nethttp "knative.dev/networking/pkg/http"
	"knative.dev/pkg/network"
)

// InitHandlers initializes all handlers.
func InitHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/", withHeaders(withRequestLog(runtimeHandler)))
	mux.HandleFunc(nethttp.HealthCheckPath, withRequestLog(withKubeletProbeHeaderCheck))
}

// withRequestLog logs each request before handling it.
func withRequestLog(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqDump, err := httputil.DumpRequest(r, true)
		if err != nil {
			log.Println(err)
		} else {
			log.Println(string(reqDump))
		}

		next.ServeHTTP(w, r)
	}
}

// withKubeletProbeHeaderCheck checks each health request has Kubelet probe header
func withKubeletProbeHeaderCheck(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("User-Agent"), network.KubeProbeUAPrefix) {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}

// setHeaders injects headers on the responses.
func withHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	}
}

// WriteObject write content to response.
func writeJSON(w http.ResponseWriter, o interface{}) {
	w.WriteHeader(http.StatusOK)
	e := json.NewEncoder(w)
	e.SetIndent("", "\t")
	e.Encode(o)
}
