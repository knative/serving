/*
Copyright 2026 The Knative Authors

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

package net

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var scopeName = "knative.dev/serving/pkg/activator"

// throttlerMetrics holds the metrics for the throttler.
type throttlerMetrics struct {
	// queueDepthObserver is used to unregister the callback when the throttler is stopped.
	queueDepthObserver metric.Registration
}

// QueueDepthProvider is an interface for types that can provide queue depth information.
type QueueDepthProvider interface {
	// QueueDepth returns a snapshot of queue depths per revision.
	// The returned map keys are "namespace/name" formatted revision identifiers.
	QueueDepth() map[string]int
}

// newThrottlerMetrics creates metrics for the throttler using an observable gauge.
// This approach has zero overhead on the request path - values are only read when
// metrics are scraped.
func newThrottlerMetrics(mp metric.MeterProvider, provider QueueDepthProvider) (*throttlerMetrics, error) {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	meter := mp.Meter(scopeName)

	m := &throttlerMetrics{}

	queueDepth, err := meter.Int64ObservableGauge(
		"kn.activator.queue.depth",
		metric.WithDescription("Number of requests currently queued in the activator for a revision"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	m.queueDepthObserver, err = meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			for revKey, depth := range provider.QueueDepth() {
				o.ObserveInt64(queueDepth, int64(depth),
					metric.WithAttributes(attribute.String("revision_key", revKey)))
			}
			return nil
		},
		queueDepth,
	)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// Shutdown unregisters the metrics callback.
func (m *throttlerMetrics) Shutdown() error {
	if m.queueDepthObserver != nil {
		return m.queueDepthObserver.Unregister()
	}
	return nil
}
