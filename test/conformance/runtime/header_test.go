// +build e2e

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

package runtime

import (
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/test"
)

const (
	userHeaderKey   = "this-was-user-set"
	userHeaderValue = "a value"
)

// TestMustHaveHeadersSet verified that all headers declared as "MUST" in the runtime
// contract are present from the point of view of the user container.
func TestMustHaveHeadersSet(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	_, ri, err := fetchRuntimeInfo(t, clients)
	if err != nil {
		t.Fatalf("Error fetching runtime info: %v", err)
	}

	// For incoming requests, the Host header is promoted to the
	// Request.Host field and removed from the Header map. Therefore we
	// check against the Host field instead of the map.
	if ri.Request.Host == "" {
		// We just check that the host header exists and is non-empty as the request
		// may be made internally or externally which will result in a different host.
		t.Error("Header host was not present on request")
	}

	expectedHeaders := map[string]stringMatcher{
		// We expect the forwarded header to be key-value pairs separated by commas and semi-colons, where
		// the allowed keys are `for`, `by`, `proto` and `host` and values are loosely validated by shape.
		// See https://tools.ietf.org/html/rfc7239#section-4 for the full syntax rules.
		"forwarded": &checkForwardedHeader{expected: "valid Forwarded header per RFC7239"},
	}

	headers := ri.Request.Headers

	matchHeaders(t, headers, expectedHeaders)
}

// TestMustHaveHeadersSet verified that all headers declared as "SHOULD" in the runtime
// contract are present from the point of view of the user container.
func TestShouldHaveHeadersSet(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	userHeaders := make(http.Header)
	userHeaders.Add(userHeaderKey, userHeaderValue)

	expectedHeaders := map[string]stringMatcher{
		// We expect user headers to be passed through exactly as-is.
		userHeaderKey: regexp.MustCompile("^" + userHeaderValue + "$"),
		// We expect the protocol to be http for our test image.
		"x-forwarded-proto": regexp.MustCompile("https?"),
		// We expect the value to be a list of at least one comma separated IP addresses (IPv4 or IPv6).
		"x-forwarded-for": &checkIPList{expected: "comma separated IPv4 or IPv6 addresses"},

		// Trace Headers
		// See https://github.com/openzipkin/b3-propagation#overall-process
		// We use the multiple header variant for tracing. We do not validate the single header variant.
		// We expect the value to be a 64-bit hex string
		"x-b3-spanid": regexp.MustCompile("[0-9a-f]{16}"),
		// We expect the value to be a 64-bit or 128-bit hex string
		"x-b3-traceid": regexp.MustCompile("[0-9a-f]{16}|[0-9a-f]{32}"),

		// "x-b3-parentspanid" and "x-b3-sampled" are often present for tracing, but are not
		// required for tracing so we do not validate them.
	}

	_, ri, err := fetchRuntimeInfo(t, clients, pkgTest.WithHeader(userHeaders))
	if err != nil {
		t.Fatalf("Error fetching runtime info: %v", err)
	}

	headers := ri.Request.Headers

	matchHeaders(t, headers, expectedHeaders)
}

type checkIPList struct {
	expected string
}

// MatchString returns true if the passed string is a list of IPv4 or IPv6 Addresses. Otherwise returns false.
func (*checkIPList) MatchString(s string) bool {
	for _, ip := range strings.Split(s, ",") {
		if net.ParseIP(strings.TrimSpace(ip)) == nil {
			return false
		}
	}
	return true
}

// String returns the expected string from the object.
func (c *checkIPList) String() string {
	return c.expected
}

type checkForwardedHeader struct {
	expected string
}

var (
	// token as defined in https://tools.ietf.org/html/rfc7230#section-3.2.6
	tokenMatcher = regexp.MustCompile(`^[0-9a-zA-Z!#$%&'*+-.^_|~]+$`)
	// approximation of quoted-string as defined in https://tools.ietf.org/html/rfc7230#section-3.2.6
	quotedStringMatcher = regexp.MustCompile(`^"[^"]*"$`)
)

func isDelimiter(r rune) bool {
	return r == ';' || r == ','
}

// MatchString returns true if the passed string contains a roughly valid Forwarded header content.
func (*checkForwardedHeader) MatchString(s string) bool {
	for _, pair := range strings.FieldsFunc(s, isDelimiter) {
		// Allow for a trailing delimiter. Some routers unfortunately do that.
		if pair == "" {
			continue
		}
		parts := strings.Split(strings.TrimSpace(pair), "=")
		if len(parts) < 2 {
			return false
		}
		token := parts[0]
		value := parts[1]

		if !tokenMatcher.MatchString(token) {
			return false
		}

		if value != "" && !(tokenMatcher.MatchString(value) || quotedStringMatcher.MatchString(value)) {
			return false
		}
	}
	return true
}

// String returns the expected string from the object.
func (c *checkForwardedHeader) String() string {
	return c.expected
}

type stringMatcher interface {
	MatchString(string) bool
	String() string
}

func matchHeaders(t *testing.T, headers http.Header, expectedHeaders map[string]stringMatcher) {
	for header, match := range expectedHeaders {
		hvl, ok := headers[http.CanonicalHeaderKey(header)]
		if !ok {
			t.Errorf("Header %s was not present on request", header)
			continue
		}
		// Check against each value for the header key
		for _, hv := range hvl {
			if !match.MatchString(hv) {
				t.Errorf("%s = %s; want: %s", header, hv, match.String())
			}
		}
	}
}
