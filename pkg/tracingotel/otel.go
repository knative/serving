/*
Copyright 2022 The Knative Authors

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

package tracingotel

import (
	"context"
	"errors"
	"fmt"
	"io"

	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"knative.dev/serving/pkg/tracingotel/config"
)

var (
	globalTracerProvider *sdktrace.TracerProvider
	globalClosers        []io.Closer
	globalZipkinExporter *zipkin.Exporter
)

// Init configures the global tracer provider based on the provided configuration.
// Call this from main() or tests. serviceName is used for the service.name resource.
// cfg drives backend (Zipkin) and sampling. logger is for exporter logging.
// extraProcessors can be used for testing (e.g., in-memory exporter).
func Init(serviceName string, cfg *config.Config, logger *zap.SugaredLogger, extraProcessors ...sdktrace.SpanProcessor) error {
	if globalTracerProvider != nil {
		if err := Shutdown(context.Background()); err != nil {
			if logger != nil {
				logger.Errorw("Failed to shutdown existing tracer provider during re-init", zap.Error(err))
			}
		}
	}
	globalZipkinExporter = nil

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	var sampler sdktrace.Sampler
	if cfg.Debug {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate > 0 && cfg.SampleRate <= 1.0 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	} else {
		sampler = sdktrace.NeverSample()
	}
	sampler = sdktrace.ParentBased(sampler)

	spanProcessors := []sdktrace.SpanProcessor{}
	spanProcessors = append(spanProcessors, extraProcessors...)

	closers := []io.Closer{}

	if cfg.Backend == config.Zipkin && cfg.ZipkinEndpoint != "" {
		if logger == nil {
			return errors.New("logger is required for Zipkin exporter")
		}

		// Desugar back to the base *zap.Logger.
		base := logger.Desugar()

		// Route std‑lib logging to Zap at INFO (choose any zapcore.Level you want).
		stdlog, err := zap.NewStdLogAt(base, zapcore.InfoLevel)
		if err != nil {
			return fmt.Errorf("failed to create stdlog: %w", err)
		}

		zkExporter, err := zipkin.New(
			cfg.ZipkinEndpoint,
			zipkin.WithLogger(stdlog),
		)
		if err != nil {
			return fmt.Errorf("failed to create zipkin exporter: %w", err)
		}
		spanProcessors = append(spanProcessors, sdktrace.NewBatchSpanProcessor(zkExporter))
		globalZipkinExporter = zkExporter
	} else if cfg.Backend != config.None && cfg.Backend != config.Zipkin {
		if logger != nil {
			logger.Warnf("Unsupported tracing backend configured: %v. Tracing will be effectively disabled unless extra processors are provided.", cfg.Backend)
		}
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}
	for _, sp := range spanProcessors {
		tpOpts = append(tpOpts, sdktrace.WithSpanProcessor(sp))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)

	otel.SetTracerProvider(tp)
	// Set the B3 propagator which is commonly used with Zipkin
	otel.SetTextMapPropagator(b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader | b3.B3SingleHeader)))

	globalTracerProvider = tp
	globalClosers = closers

	return nil
}

// Tracer returns the package tracer.
func Tracer() trace.Tracer {
	return otel.Tracer("knative.dev/serving/pkg/tracingotel")
}

// Shutdown flushes and shuts down the provider and any registered closers.
func Shutdown(ctx context.Context) error {
	var firstErr error

	if globalZipkinExporter != nil {
		if err := globalZipkinExporter.Shutdown(ctx); err != nil {
			firstErr = fmt.Errorf("failed to shutdown zipkin exporter: %w", err)
		}
		globalZipkinExporter = nil
	}

	for _, closer := range globalClosers {
		if err := closer.Close(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to close a global closer: %w", err)
			} else {
				fmt.Printf("Additional error during shutdown of closers: %v\n", err)
			}
		}
	}
	globalClosers = nil

	if globalTracerProvider != nil {
		if err := globalTracerProvider.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("otel tracer provider shutdown: %w", err)
			}
		} else {
			otel.SetTracerProvider(noop.NewTracerProvider())
			globalTracerProvider = nil
		}
	}
	return firstErr
}
