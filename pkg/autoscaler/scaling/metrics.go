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

package scaling

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const scopeName = "knative.dev/serving/pkg/autoscaler"

type scalingMetrics struct {
	attrs               attribute.Set
	desiredPod          metric.Int64Gauge
	excessBurstCapacity metric.Float64Gauge

	stableConcurrency metric.Float64Gauge
	panicConcurrency  metric.Float64Gauge
	targetConcurrency metric.Float64Gauge

	stableRPS metric.Float64Gauge
	panicRPS  metric.Float64Gauge
	targetRPS metric.Float64Gauge

	panicMode metric.Int64Gauge
}

func newMetrics(mp metric.MeterProvider, attrs attribute.Set) *scalingMetrics {
	var (
		m = scalingMetrics{attrs: attrs}
		p = mp
	)

	if p == nil {
		p = otel.GetMeterProvider()
	}

	meter := p.Meter(scopeName)

	m.desiredPod = must(meter.Int64Gauge(
		"kn.revision.pods.desired",
		metric.WithDescription("Number of pods the autoscaler wants to allocate"),
		metric.WithUnit("{item}"),
	))

	m.excessBurstCapacity = must(meter.Float64Gauge(
		"kn.revision.capacity.excess",
		metric.WithDescription("Excess burst capacity observed over the stable window"),
		metric.WithUnit("{concurrency}"),
	))

	m.stableConcurrency = must(meter.Float64Gauge(
		"kn.revision.concurrency.stable",
		metric.WithDescription("Average of request count per observed pod over the stable window"),
		metric.WithUnit("{concurrency}"),
	))

	m.panicConcurrency = must(meter.Float64Gauge(
		"kn.revision.concurrency.panic",
		metric.WithDescription("Average of request count per observed pod over the panic window"),
		metric.WithUnit("{concurrency}"),
	))

	m.targetConcurrency = must(meter.Float64Gauge(
		"kn.revision.concurrency.target",
		metric.WithDescription("The desired concurrent requests for each pod"),
		metric.WithUnit("{concurrency}"),
	))

	m.stableRPS = must(meter.Float64Gauge(
		"kn.revision.rps.stable",
		metric.WithDescription("Average of requests-per-second per observed pod over the stable window"),
		metric.WithUnit("{request}/s"),
	))

	m.panicRPS = must(meter.Float64Gauge(
		"kn.revision.rps.panic",
		metric.WithDescription("Average of requests-per-second per observed pod over the panic window"),
		metric.WithUnit("{request}/s"),
	))

	m.targetRPS = must(meter.Float64Gauge(
		"kn.revision.rps.target",
		metric.WithDescription("The desired concurrent requests for each pod"),
		metric.WithUnit("{request}/s"),
	))

	m.panicMode = must(meter.Int64Gauge(
		"kn.revision.panic.mode",
		metric.WithDescription("If greater tha 0 the autoscaler is in panic mode"),
	))

	return &m
}

func (m *scalingMetrics) SetPanic(panic bool) {
	if m == nil {
		return
	}

	ctx := context.Background()
	val := int64(0)
	if panic {
		val = 1
	}

	m.panicMode.Record(ctx, val, metric.WithAttributeSet(m.attrs))
}

func (m *scalingMetrics) RecordRPS(
	excessBurstCapacity float64,
	desiredPods int64,
	stableRPS float64,
	panicRPS float64,
	targetRPS float64,
) {
	if m == nil {
		return
	}

	opt := metric.WithAttributeSet(m.attrs)
	ctx := context.Background()

	m.excessBurstCapacity.Record(ctx, excessBurstCapacity, opt)
	m.desiredPod.Record(ctx, desiredPods, opt)
	m.stableRPS.Record(ctx, stableRPS, opt)
	m.panicRPS.Record(ctx, panicRPS, opt)
	m.targetRPS.Record(ctx, targetRPS, opt)
}

func (m *scalingMetrics) RecordConcurrency(
	excessBurstCapacity float64,
	desiredPods int64,
	stableConcurrency float64,
	panicConcurrency float64,
	targetConcurrency float64,
) {
	if m == nil {
		return
	}

	opt := metric.WithAttributeSet(m.attrs)
	ctx := context.Background()
	m.excessBurstCapacity.Record(ctx, excessBurstCapacity, opt)
	m.desiredPod.Record(ctx, desiredPods, opt)
	m.stableConcurrency.Record(ctx, stableConcurrency, opt)
	m.panicConcurrency.Record(ctx, panicConcurrency, opt)
	m.targetConcurrency.Record(ctx, targetConcurrency, opt)
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}
