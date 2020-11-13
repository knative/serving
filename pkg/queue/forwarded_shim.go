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

// ForwardedShimHandler attempts to shim a `forwarded` HTTP header from the information
// available in the `x-forwarded-*` headers. When available, each node in the `x-forwarded-for`
// header is combined with the `x-forwarded-proto` and `x-forwarded-host` fields to construct
// a `forwarded` header. The `x-forwarded-by` header is ignored entirely, since it cannot be
// reliably combined with `x-forwarded-for`. No-op if a `forwarded` header is already present.
func ForwardedShimHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer h.ServeHTTP(w, r)

		// Forwarded: by=<identifier>;for=<identifier>;host=<host>;proto=<http|https>
		fwd := r.Header.Get("Forwarded")

		// Don't add a shim if the header is already present
		if fwd != "" {
			return
		}

		// X-Forwarded-For: <client>, <proxy1>, <proxy2>
		xff := r.Header.Get("X-Forwarded-For")
		// X-Forwarded-Proto: <protocol>
		xfp := r.Header.Get("X-Forwarded-Proto")
		// X-Forwarded-Host: <host>
		xfh := r.Header.Get("X-Forwarded-Host")

		// Nothing to do if we don't have any X-Forwarded-* headers
		if xff == "" && xfp == "" && xfh == "" {
			return
		}

		r.Header.Set("Forwarded", generateForwarded(xff, xfp, xfh))
	})
}

func generateForwarded(xff, xfp, xfh string) string {
	fwd := &strings.Builder{}
	// The size is dominated by the side of the individual headers.
	// + 5 + 1 for host= and delimiter
	// + 6 + 1 for proto= and delimiter
	// + (5 + 4) * x for each for= clause and delimiter (assuming ipv6)
	fwd.Grow(len(xff) + len(xfp) + len(xfh) + 6 + 7 + 9*(strings.Count(xff, ",")+1))

	node, next := consumeNode(xff, 0)
	if xff != "" {
		fwd.WriteString("for=")
		writeNode(fwd, node)
	}
	if xfh != "" {
		if node != "" {
			fwd.WriteRune(';')
		}
		fwd.WriteString("host=")
		fwd.WriteString(xfh)
	}
	if xfp != "" {
		if node != "" || xfh != "" {
			fwd.WriteRune(';')
		}
		fwd.WriteString("proto=")
		fwd.WriteString(xfp)
	}

	for next < len(xff) {
		node, next = consumeNode(xff, next)
		fwd.WriteString(", for=")
		writeNode(fwd, node)
	}

	return fwd.String()
}

func consumeNode(xff string, from int) (string, int) {
	if xff == "" {
		return "", 0
	}
	// The x-forwarded-header consists of multiple nodes, split by ","
	rest := xff[from:]
	i := strings.Index(rest, ",")
	if i == -1 {
		i = len(rest)
	}
	return strings.TrimSpace(rest[:i]), from + i + 1
}

func writeNode(fwd *strings.Builder, node string) {
	// For simplicity, an address is IPv6 it contains a colon (:)
	ipv6 := strings.Contains(node, ":")
	if ipv6 {
		// Convert IPv6 address to "[ipv6 addr]" format
		fwd.WriteString(`"[`)
	}
	fwd.WriteString(node)
	if ipv6 {
		fwd.WriteString(`]"`)
	}
}
