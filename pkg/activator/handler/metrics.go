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

package handler

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var scopeName = "knative.dev/serving/pkg/activator"

type ccMetrics struct {
	requestCC metric.Float64Gauge
}

func newMetrics(mp metric.MeterProvider) *ccMetrics {
	var (
		m        ccMetrics
		err      error
		provider = mp
	)

	if provider == nil {
		provider = otel.GetMeterProvider()
	}

	meter := provider.Meter(scopeName)

	m.requestCC, err = meter.Float64Gauge(
		"kn.revision.request.concurrency",
		metric.WithDescription("Concurrent requests that are routed to Activator"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(err)
	}

	return &m
}

// requestMetrics holds metrics for tracking request states in the activator.
type requestMetrics struct {
	requestQueued metric.Int64UpDownCounter
	requestActive metric.Int64UpDownCounter
}

func newRequestMetrics(mp metric.MeterProvider) *requestMetrics {
	var (
		m        requestMetrics
		err      error
		provider = mp
	)

	if provider == nil {
		provider = otel.GetMeterProvider()
	}

	meter := provider.Meter(scopeName)

	m.requestQueued, err = meter.Int64UpDownCounter(
		"kn.revision.request.queued",
		metric.WithDescription("Number of requests currently queued in the activator waiting for capacity"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(err)
	}

	m.requestActive, err = meter.Int64UpDownCounter(
		"kn.revision.request.active",
		metric.WithDescription("Number of requests currently being proxied by the activator"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		panic(err)
	}

	return &m
}
