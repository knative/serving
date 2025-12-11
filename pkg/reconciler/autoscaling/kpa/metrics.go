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
	opt          otelmetric.MeasurementOption
	registration otelmetric.Registration

	requestedPods   otelmetric.Int64ObservableGauge
	actualPods      otelmetric.Int64ObservableGauge
	notReadyPods    otelmetric.Int64ObservableGauge
	pendingPods     otelmetric.Int64ObservableGauge
	terminatingPods otelmetric.Int64ObservableGauge

	pc podCounts
}

func (m *kpaMetrics) callback(ctx context.Context, o otelmetric.Observer) error {
	o.ObserveInt64(m.requestedPods, int64(m.pc.want), m.opt)
	o.ObserveInt64(m.actualPods, int64(m.pc.ready), m.opt)
	o.ObserveInt64(m.notReadyPods, int64(m.pc.notReady), m.opt)
	o.ObserveInt64(m.pendingPods, int64(m.pc.pending), m.opt)
	o.ObserveInt64(m.terminatingPods, int64(m.pc.terminating), m.opt)

	return nil
}

func (m *kpaMetrics) OnDelete() {
	if m == nil {
		return
	}

	m.registration.Unregister()
}

func newMetrics(mp otelmetric.MeterProvider, attrs attribute.Set) *kpaMetrics {
	var (
		m = kpaMetrics{
			opt: otelmetric.WithAttributeSet(attrs),
		}
		p = mp
	)

	if p == nil {
		p = otel.GetMeterProvider()
	}

	meter := p.Meter(scopeName)

	m.requestedPods = must(meter.Int64ObservableGauge(
		"kn.revision.pods.requested",
		otelmetric.WithDescription("Number of pods autoscaler requested from Kubernetes"),
		otelmetric.WithUnit("{pod}"),
	))

	m.actualPods = must(meter.Int64ObservableGauge(
		"kn.revision.pods.count",
		otelmetric.WithDescription("Number of pods that are allocated currently"),
		otelmetric.WithUnit("{pod}"),
	))

	m.notReadyPods = must(meter.Int64ObservableGauge(
		"kn.revision.pods.not_ready.count",
		otelmetric.WithDescription("Number of pods that are not ready currently"),
		otelmetric.WithUnit("{pod}"),
	))

	m.pendingPods = must(meter.Int64ObservableGauge(
		"kn.revision.pods.pending.count",
		otelmetric.WithDescription("Number of pods that are pending currently"),
		otelmetric.WithUnit("{pod}"),
	))

	m.terminatingPods = must(meter.Int64ObservableGauge(
		"kn.revision.pods.terminating.count",
		otelmetric.WithDescription("Number of pods that are terminating currently"),
		otelmetric.WithUnit("{pod}"),
	))

	m.registration = must(meter.RegisterCallback(m.callback,
		m.requestedPods,
		m.actualPods,
		m.notReadyPods,
		m.pendingPods,
		m.terminatingPods,
	))

	return &m
}

func (m *kpaMetrics) Record(pc podCounts) {
	if m == nil {
		return
	}

	// Preserve the prior value if we don't know the scale
	if pc.want == scaleUnknown {
		pc.want = m.pc.want
	}

	m.pc = pc
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}
