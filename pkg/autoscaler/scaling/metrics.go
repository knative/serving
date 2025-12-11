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
	"knative.dev/serving/pkg/apis/autoscaling"
)

const scopeName = "knative.dev/serving/pkg/autoscaler"

type scalingMetrics struct {
	attrs        attribute.Set
	registration metric.Registration

	desiredPods         metric.Int64ObservableGauge
	excessBurstCapacity metric.Float64ObservableGauge
	panicMode           metric.Int64ObservableGauge

	panicMetric  metric.Float64ObservableGauge
	stableMetric metric.Float64ObservableGauge
	targetMetric metric.Float64ObservableGauge

	panicValue  float64
	stableValue float64
	targetValue float64

	desiredPodsValue         int64
	excessBurstCapacityValue float64
	panicModeValue           int64
}

func (m *scalingMetrics) OnDelete() {
	if m == nil {
		return
	}

	m.registration.Unregister()
}

func (m *scalingMetrics) callback(ctx context.Context, o metric.Observer) error {
	opt := metric.WithAttributeSet(m.attrs)

	o.ObserveInt64(m.desiredPods, m.desiredPodsValue, opt)
	o.ObserveFloat64(m.excessBurstCapacity, m.excessBurstCapacityValue, opt)
	o.ObserveInt64(m.panicMode, m.panicModeValue, opt)

	o.ObserveFloat64(m.panicMetric, m.panicValue, opt)
	o.ObserveFloat64(m.stableMetric, m.stableValue, opt)
	o.ObserveFloat64(m.targetMetric, m.targetValue, opt)

	return nil
}

func newMetrics(mp metric.MeterProvider, scalingMetric string, attrs attribute.Set) *scalingMetrics {
	var (
		m = &scalingMetrics{attrs: attrs}
		p = mp
	)

	if p == nil {
		p = otel.GetMeterProvider()
	}

	meter := p.Meter(scopeName)

	m.desiredPods = must(meter.Int64ObservableGauge(
		"kn.revision.pods.desired",
		metric.WithDescription("Number of pods the autoscaler wants to allocate"),
		metric.WithUnit("{item}"),
	))

	m.excessBurstCapacity = must(meter.Float64ObservableGauge(
		"kn.revision.capacity.excess",
		metric.WithDescription("Excess burst capacity observed over the stable window"),
		metric.WithUnit("{concurrency}"),
	))

	m.panicMode = must(meter.Int64ObservableGauge(
		"kn.revision.panic.mode",
		metric.WithDescription("If greater tha 0 the autoscaler is in panic mode"),
	))

	switch scalingMetric {
	case autoscaling.RPS:
		m.stableMetric = must(meter.Float64ObservableGauge(
			"kn.revision.rps.stable",
			metric.WithDescription("Average of requests-per-second per observed pod over the stable window"),
			metric.WithUnit("{request}/s"),
		))
		m.panicMetric = must(meter.Float64ObservableGauge(
			"kn.revision.rps.panic",
			metric.WithDescription("Average of requests-per-second per observed pod over the panic window"),
			metric.WithUnit("{request}/s"),
		))
		m.targetMetric = must(meter.Float64ObservableGauge(
			"kn.revision.rps.target",
			metric.WithDescription("The desired concurrent requests for each pod"),
			metric.WithUnit("{request}/s"),
		))
	default:
		m.stableMetric = must(meter.Float64ObservableGauge(
			"kn.revision.concurrency.stable",
			metric.WithDescription("Average of request count per observed pod over the stable window"),
			metric.WithUnit("{concurrency}"),
		))
		m.panicMetric = must(meter.Float64ObservableGauge(
			"kn.revision.concurrency.panic",
			metric.WithDescription("Average of request count per observed pod over the panic window"),
			metric.WithUnit("{concurrency}"),
		))
		m.targetMetric = must(meter.Float64ObservableGauge(
			"kn.revision.concurrency.target",
			metric.WithDescription("The desired concurrent requests for each pod"),
			metric.WithUnit("{concurrency}"),
		))
	}

	m.registration = must(meter.RegisterCallback(m.callback,
		m.desiredPods,
		m.excessBurstCapacity,
		m.panicMode,
		m.stableMetric,
		m.panicMetric,
		m.targetMetric,
	))

	return m
}

func (m *scalingMetrics) SetPanic(panic bool) {
	if m == nil {
		return
	}

	val := int64(0)
	if panic {
		val = 1
	}

	m.panicModeValue = val
}

func (m *scalingMetrics) Record(
	excessBurstCapacity float64,
	desiredPods int64,
	stableRPS float64,
	panicRPS float64,
	targetRPS float64,
) {
	if m == nil {
		return
	}

	m.desiredPodsValue = desiredPods
	m.excessBurstCapacityValue = excessBurstCapacity
	m.stableValue = stableRPS
	m.panicValue = panicRPS
	m.targetValue = targetRPS
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}
