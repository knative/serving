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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var scopeName = "knative.dev/serving/pkg/activator"

type statReporterMetrics struct {
	autoscalerReachable        metric.Int64Gauge
	autoscalerConnectionErrors metric.Int64Counter
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

	meter := provider.Meter(scopeName)

	m.autoscalerReachable, err = meter.Int64Gauge(
		"kn.activator.autoscaler.reachable",
		metric.WithDescription("Whether the autoscaler is reachable from the activator (1 = reachable, 0 = not reachable)"),
		metric.WithUnit("{reachable}"),
	)
	if err != nil {
		panic(err)
	}

	m.autoscalerConnectionErrors, err = meter.Int64Counter(
		"kn.activator.autoscaler.connection_errors_total",
		metric.WithDescription("Total number of autoscaler connection errors from the activator"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		panic(err)
	}

	return &m
}
