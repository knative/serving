//go:build e2e
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
	"flag"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	pkgTest "knative.dev/pkg/test"
)

func TestMain(m *testing.M) {
	flag.Parse()

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()), // never record or export
	)
	// Set the global TracerProvider (so instrumentations use it).
	otel.SetTracerProvider(tp)

	// 2) Make sure we have a propagator (W3C tracecontext is default, but set explicitly).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	pkgTest.SetupLoggingFlags()
	os.Exit(m.Run())
}
