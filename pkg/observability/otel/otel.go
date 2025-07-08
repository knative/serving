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

package otel

import (
	"context"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"

	"go.uber.org/zap"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stoolmetrics "k8s.io/client-go/tools/metrics"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/controller"
	o11yconfigmap "knative.dev/pkg/observability/configmap"
	"knative.dev/pkg/observability/metrics"
	k8smetrics "knative.dev/pkg/observability/metrics/k8s"
	"knative.dev/pkg/observability/resource"
	k8sruntime "knative.dev/pkg/observability/runtime/k8s"
	"knative.dev/pkg/observability/tracing"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/observability"
)

// SetupObservabilityOrDie sets up the observability using the config from the given context
// or dies by calling log.Fatalf.
func SetupObservabilityOrDie(
	ctx context.Context,
	component string,
	logger *zap.SugaredLogger,
	pprof *k8sruntime.ProfilingServer,
) (*metrics.MeterProvider, *tracing.TracerProvider) {
	cfg, err := GetObservabilityConfig(ctx)
	if err != nil {
		logger.Fatal("Error loading observability configuration: ", err)
	}

	pprof.UpdateFromConfig(cfg.Runtime)

	resource := resource.Default(component)

	meterProvider, err := metrics.NewMeterProvider(
		ctx,
		cfg.Metrics,
		metric.WithResource(resource),
	)
	if err != nil {
		logger.Fatalw("Failed to setup meter provider", zap.Error(err))
	}

	otel.SetMeterProvider(meterProvider)

	workQueueMetrics, err := k8smetrics.NewWorkqueueMetricsProvider(
		k8smetrics.WithMeterProvider(meterProvider),
	)
	if err != nil {
		logger.Fatalw("Failed to setup k8s workqueue metrics", zap.Error(err))
	}

	workqueue.SetProvider(workQueueMetrics)
	controller.SetMetricsProvider(workQueueMetrics)

	clientMetrics, err := k8smetrics.NewClientMetricProvider(
		k8smetrics.WithMeterProvider(meterProvider),
	)
	if err != nil {
		logger.Fatalw("Failed to setup k8s client go metrics", zap.Error(err))
	}

	k8stoolmetrics.Register(k8stoolmetrics.RegisterOpts{
		RequestLatency: clientMetrics.RequestLatencyMetric(),
		RequestResult:  clientMetrics.RequestResultMetric(),
	})

	err = runtime.Start(
		runtime.WithMinimumReadMemStatsInterval(cfg.Runtime.ExportInterval),
	)
	if err != nil {
		logger.Fatalw("Failed to start runtime metrics", zap.Error(err))
	}

	tracerProvider, err := tracing.NewTracerProvider(
		ctx,
		cfg.Tracing,
		trace.WithResource(resource),
	)
	if err != nil {
		logger.Fatalw("Failed to setup trace provider", zap.Error(err))
	}

	otel.SetTextMapPropagator(tracing.DefaultTextMapPropagator())
	otel.SetTracerProvider(tracerProvider)

	return meterProvider, tracerProvider
}

// GetObservabilityConfig gets the observability config from the (in order):
// 1. provided context,
// 2. reading from the API server,
// 3. defaults (if not found).
func GetObservabilityConfig(ctx context.Context) (*observability.Config, error) {
	if cfg := observability.GetConfig(ctx); cfg != nil {
		return cfg, nil
	}

	client := kubeclient.Get(ctx).CoreV1().ConfigMaps(system.Namespace())
	cm, err := client.Get(ctx, o11yconfigmap.Name(), metav1.GetOptions{})

	if apierrors.IsNotFound(err) {
		return observability.DefaultConfig(), nil
	} else if err != nil {
		return nil, err
	}

	return observability.NewFromMap(cm.Data)
}
