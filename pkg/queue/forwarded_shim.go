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

package queue

import (
	"net/http"
	"strings"
)

// ForwardedShimHandler attempts to shim a `forwarded` http header from the information
// available in the `x-forwarded-*` headers. When available, each node in the `x-forwarded-for`
// header is combined with the `x-forwarded-proto` and `x-forwarded-host` fields to construct
// a `forwarded` header. The `x-forwarded-by` header is ignored entirely, since it cannot be
// reliably combined with `x-forwarded-for`. No-op if a `forwarded` header is already present.
// Note: IPv6 addresses are *not* supported.
func ForwardedShimHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Forwarded: by=<identifier>;for=<identifier>;host=<host>;proto=<http|https>
		fwd := strings.TrimSpace(r.Header.Get(http.CanonicalHeaderKey("forwarded")))

		// Don't add a shim if the header is already present
		if fwd != "" {
			h.ServeHTTP(w, r)
			return
		}

		// X-Forwarded-For: <client>, <proxy1>, <proxy2>
		xff := strings.TrimSpace(r.Header.Get(http.CanonicalHeaderKey("x-forwarded-for")))
		// X-Forwarded-Proto: <protocol>
		xfp := strings.TrimSpace(r.Header.Get(http.CanonicalHeaderKey("x-forwarded-proto")))
		// X-Forwarded-Host: <host>
		xfh := strings.TrimSpace(r.Header.Get(http.CanonicalHeaderKey("x-forwarded-host")))

		// The forwarded header is a list of forwarded elements
		elements := []string{}

		// The x-forwarded-header consists of multiple nodes
		nodes := strings.Split(xff, ",")

		// The first element has a 'for', 'proto' and 'host' pair, as available
		pairs := []string{}

		if len(nodes) > 0 && nodes[0] != "" {
			pairs = append(pairs, "for="+strings.TrimSpace(nodes[0]))
		}
		if xfh != "" {
			pairs = append(pairs, "host="+xfh)
		}
		if xfp != "" {
			pairs = append(pairs, "proto="+xfp)
		}

		// The pairs are joined with a semi-colon (;) into a single element
		elements = append(elements, strings.Join(pairs, ";"))

		// Each subsequent x-forwarded-for node gets its own pair element
		for _, node := range nodes[1:] {
			elements = append(elements, "for="+strings.TrimSpace(node))
		}

		// The elements are joined with a comma (,) to form the header
		fwd = strings.Join(elements, ", ")

		// Only add the forwarded header if we were able to construct one
		if fwd != "" {
			r.Header.Set(http.CanonicalHeaderKey("forwarded"), fwd)
		}

		h.ServeHTTP(w, r)
	})
}
