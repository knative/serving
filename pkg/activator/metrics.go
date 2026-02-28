/*
Copyright 2024 The Knative Authors

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

package activator

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"knative.dev/pkg/observability/attributekey"
)

var scopeName = "knative.dev/serving/pkg/activator"

// peerAttrKey is the attribute key for identifying the connection peer.
var peerAttrKey = attributekey.String("peer")

// PeerAutoscaler is the attribute value for autoscaler connections.
var PeerAutoscaler = peerAttrKey.With("autoscaler")

type statReporterMetrics struct {
	reachable        metric.Int64Gauge
	connectionErrors metric.Int64Counter

	autoscalerPeer attribute.Set
}

func newStatReporterMetrics(mp metric.MeterProvider) *statReporterMetrics {
	var (
		m        statReporterMetrics
		err      error
		provider = mp
	)

	if provider == nil {
		provider = otel.GetMeterProvider()
	}

	m.autoscalerPeer = attribute.NewSet(PeerAutoscaler)
	meter := provider.Meter(scopeName)

	m.reachable, err = meter.Int64Gauge(
		"kn.activator.stats.conn.reachable",
		metric.WithDescription("Whether a peer is reachable from the activator (1 = reachable, 0 = not reachable)"),
		metric.WithUnit("{reachable}"),
	)
	if err != nil {
		panic(err)
	}

	m.connectionErrors, err = meter.Int64Counter(
		"kn.activator.stats.conn.errors",
		metric.WithDescription("Number of connection errors from the activator"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		panic(err)
	}

	return &m
}

func (s *statReporterMetrics) OnAutoscalerConnect() {
	ctx := context.Background()
	s.reachable.Record(ctx, 1, metric.WithAttributes(PeerAutoscaler))
}

func (s *statReporterMetrics) OnAutoscalerDisconnect() {
	ctx := context.Background()
	s.reachable.Record(ctx, 0, metric.WithAttributeSet(s.autoscalerPeer))
	s.connectionErrors.Add(ctx, 1, metric.WithAttributeSet(s.autoscalerPeer))
}
