/*
Copyright 2025 The Knative Authors

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

package sharedmain

import (
	"cmp"
	"context"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.uber.org/zap"

	"knative.dev/pkg/changeset"
	"knative.dev/pkg/observability/metrics"
	"knative.dev/pkg/observability/tracing"
	"knative.dev/pkg/system"
	servingmetrics "knative.dev/serving/pkg/metrics"
)

func SetupObservabilityOrDie(
	ctx context.Context,
	cfg *config,
	logger *zap.SugaredLogger,
) (*metrics.MeterProvider, *tracing.TracerProvider) {
	r := res(logger, cfg)

	meterProvider, err := metrics.NewMeterProvider(
		ctx,
		cfg.Observability.RequestMetrics,
		metric.WithResource(r),
	)
	if err != nil {
		logger.Fatalw("Failed to setup meter provider", zap.Error(err))
	}

	otel.SetMeterProvider(meterProvider)

	err = runtime.Start(
		runtime.WithMinimumReadMemStatsInterval(cfg.Observability.Runtime.ExportInterval),
		runtime.WithMeterProvider(meterProvider),
	)
	if err != nil {
		logger.Fatalw("Failed to start runtime metrics", zap.Error(err))
	}

	tracerProvider, err := tracing.NewTracerProvider(
		ctx,
		cfg.Observability.Tracing,
		trace.WithResource(r),
	)
	if err != nil {
		logger.Fatalw("Failed to setup trace provider", zap.Error(err))
	}

	otel.SetTextMapPropagator(tracing.DefaultTextMapPropagator())
	otel.SetTracerProvider(tracerProvider)

	return meterProvider, tracerProvider
}

func res(logger *zap.SugaredLogger, cfg *config) *resource.Resource {
	serviceName := cmp.Or(
		os.Getenv("OTEL_SERVICE_NAME"),
		os.Getenv("SERVING_SERVICE"),
		os.Getenv("SERVING_CONFIGURATION"),
		os.Getenv("SERVING_REVISION"),

		// I always expect SERVING_REVISION to be set but in case it's
		// not fallback on pod name
		system.PodName(),
	)

	attrs := []attribute.KeyValue{
		semconv.K8SNamespaceName(cfg.ServingNamespace),
		semconv.ServiceVersion(changeset.Get()),
		semconv.ServiceName(serviceName),
		semconv.ServiceInstanceID(system.PodName()),
	}

	if val := os.Getenv("SERVING_SERVICE"); val != "" {
		attrs = append(attrs, servingmetrics.ServiceNameKey.With(val))
	}
	if val := os.Getenv("SERVING_CONFIGURATION"); val != "" {
		attrs = append(attrs, servingmetrics.ConfigurationNameKey.With(val))
	}
	if val := os.Getenv("SERVING_REVISION"); val != "" {
		attrs = append(attrs, servingmetrics.RevisionNameKey.With(val))
	}

	r, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			attrs...,
		),
	)
	if err != nil {
		logger.Fatalw("error merging otel resources", zap.Error(err))
	}

	return r
}
