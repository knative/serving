// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package b3 // import "go.opentelemetry.io/contrib/propagators/b3"

// Version is the current release version of the B3 propagator.
func Version() string {
	return "1.35.0"
	// This string is updated by the pre_release.sh script during release
}

// SemVersion is the semantic version to be supplied to tracer/meter creation.
//
// Deprecated: Use [Version] instead.
func SemVersion() string {
	return Version()
}
