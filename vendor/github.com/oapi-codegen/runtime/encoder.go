// Copyright 2019 DeepMap, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package runtime

import (
	"net/url"
	"strings"
)

// QueryEncoder escapes values destined for a URL query string. It governs
// query parameter values, names, and map keys; it does NOT affect path
// escaping or application/x-www-form-urlencoded request bodies, where encoding
// a space as '+' is the correct, media-type-defined behavior.
//
// Names and map keys are escaped as values with allowReserved=false, since
// allowReserved applies only to values per the OpenAPI spec.
//
// Generated clients route all query escaping through DefaultQueryEncoder.
// Provide a custom implementation, or use one of the built-ins
// (NetURLQueryEncoder, RFC3986QueryEncoder), to control how query strings are
// encoded on the wire.
type QueryEncoder interface {
	// EscapeQueryValue escapes a query parameter value. When allowReserved is
	// true, RFC 3986 reserved characters are left unencoded, per OpenAPI's
	// allowReserved option (which applies to values only).
	EscapeQueryValue(value string, allowReserved bool) string
}

// NetURLQueryEncoder preserves Go's net/url behavior, encoding a space as '+'.
// This matches the application/x-www-form-urlencoded convention that browsers
// use for HTML forms. It is the default encoder, so existing generated clients
// are unaffected. Note that many, but not all, servers accept '+' as a space.
type NetURLQueryEncoder struct{}

func (NetURLQueryEncoder) EscapeQueryValue(value string, allowReserved bool) string {
	if allowReserved {
		// The allowReserved encoding is already RFC 3986 compliant (space is
		// encoded as %20), so it is identical for both built-in encoders.
		return escapeQueryAllowReserved(value)
	}
	return url.QueryEscape(value)
}

// RFC3986QueryEncoder encodes a space as %20 rather than '+', for strict RFC
// 3986 compliance. Some servers (for example, OData endpoints that expect
// filters such as ?filter=name%20eq%20'x') reject '+'-encoded spaces with 400
// Bad Request. Set runtime.DefaultQueryEncoder to an RFC3986QueryEncoder to opt
// in.
//
// The +->%20 rewrite is lossless: url.QueryEscape encodes a literal '+' in the
// input as %2B, so any '+' remaining in its output can only be a space.
type RFC3986QueryEncoder struct{}

func (RFC3986QueryEncoder) EscapeQueryValue(value string, allowReserved bool) string {
	if allowReserved {
		return escapeQueryAllowReserved(value)
	}
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// DefaultQueryEncoder is the QueryEncoder used by all generated clients to
// escape query parameters. It defaults to NetURLQueryEncoder for backwards
// compatibility. Set it once during program initialization; like
// http.DefaultClient, it is not safe to mutate concurrently with in-flight
// requests.
var DefaultQueryEncoder QueryEncoder = NetURLQueryEncoder{}
