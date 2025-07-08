/*
Copyright 2021 The Knative Authors

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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	rtesting "knative.dev/pkg/reconciler/testing"
)

func TestTracingHandler(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		labeler, _ := otelhttp.LabelerFromContext(r.Context())
		labeler.Add(attribute.Bool("x", true))

	})
	handler := NewTracingHandler(tp, baseHandler)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	handler.ServeHTTP(resp, req.WithContext(ctx))

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Got %d spans, expected 1: spans = %v", len(spans), spans)
	}

	want := attribute.Bool("x", true)
	found := false

	for _, attr := range spans[0].Attributes {
		if attr.Key == "x" {
			found = true
			if diff := cmp.Diff(want, attr, cmpOpts...); diff != "" {
				t.Error("unexpected diff (-want +got):", diff)
			}
		}
	}

	if !found {
		t.Error("custom attribute 'x' is missing")
	}
}

var cmpOpts = []cmp.Option{
	cmpopts.EquateComparable(
		attribute.KeyValue{},
		attribute.Value{},
	),
}
