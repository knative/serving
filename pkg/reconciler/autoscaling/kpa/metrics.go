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

package kpa

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

const scopeName = "knative.dev/serving/pkg/autoscaler"

type kpaMetrics struct {
	requestedPods   otelmetric.Int64Gauge
	actualPods      otelmetric.Int64Gauge
	notReadyPods    otelmetric.Int64Gauge
	pendingPods     otelmetric.Int64Gauge
	terminatingPods otelmetric.Int64Gauge
}

func newMetrics(mp otelmetric.MeterProvider) *kpaMetrics {
	var (
		m = kpaMetrics{}
		p = mp
	)

	if p == nil {
		p = otel.GetMeterProvider()
	}

	meter := p.Meter(scopeName)

	m.requestedPods = must(meter.Int64Gauge(
		"kn.revision.pods.requested",
		otelmetric.WithDescription("Number of pods autoscaler requested from Kubernetes"),
		otelmetric.WithUnit("{pod}"),
	))

	m.actualPods = must(meter.Int64Gauge(
		"kn.revision.pods.count",
		otelmetric.WithDescription("Number of pods that are allocated currently"),
		otelmetric.WithUnit("{pod}"),
	))

	m.notReadyPods = must(meter.Int64Gauge(
		"kn.revision.pods.not_ready.count",
		otelmetric.WithDescription("Number of pods that are not ready currently"),
		otelmetric.WithUnit("{pod}"),
	))

	m.pendingPods = must(meter.Int64Gauge(
		"kn.revision.pods.pending.count",
		otelmetric.WithDescription("Number of pods that are pending currently"),
		otelmetric.WithUnit("{pod}"),
	))

	m.terminatingPods = must(meter.Int64Gauge(
		"kn.revision.pods.terminating.count",
		otelmetric.WithDescription("Number of pods that are terminating currently"),
		otelmetric.WithUnit("{pod}"),
	))

	return &m
}

func (m *kpaMetrics) Record(attrs attribute.Set, pc podCounts) {
	if m == nil {
		return
	}

	ctx := context.Background()
	opt := otelmetric.WithAttributeSet(attrs)

	if pc.want >= 0 {
		m.requestedPods.Record(ctx, int64(pc.want), opt)
	}

	m.actualPods.Record(ctx, int64(pc.ready), opt)
	m.notReadyPods.Record(ctx, int64(pc.notReady), opt)
	m.pendingPods.Record(ctx, int64(pc.pending), opt)
	m.terminatingPods.Record(ctx, int64(pc.terminating), opt)
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}
