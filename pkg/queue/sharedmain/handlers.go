/*
Copyright 2023 The Knative Authors

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
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	netheader "knative.dev/networking/pkg/http/header"
	netstats "knative.dev/networking/pkg/http/stats"
	pkghandler "knative.dev/pkg/network/handlers"
	"knative.dev/serving/pkg/http/handler"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
)

type drainers struct {
	HijackedDrainer *handler.HijackTracker
	StandardDrainer *pkghandler.Drainer
}

func mainHandler(
	env config,
	d Defaults,
	prober func() bool,
	stats *netstats.RequestStats,
	logger *zap.SugaredLogger,
	mp metric.MeterProvider,
	tp trace.TracerProvider,
) (http.Handler, drainers) {
	var drainers drainers
	tracer := tp.Tracer("knative.dev/serving/pkg/queue")
	breaker := buildBreaker(logger, env)

	timeout := time.Duration(env.RevisionTimeoutSeconds) * time.Second
	responseStartTimeout := 0 * time.Second
	if env.RevisionResponseStartTimeoutSeconds != 0 {
		responseStartTimeout = time.Duration(env.RevisionResponseStartTimeoutSeconds) * time.Second
	}
	idleTimeout := 0 * time.Second
	if env.RevisionIdleTimeoutSeconds != 0 {
		idleTimeout = time.Duration(env.RevisionIdleTimeoutSeconds) * time.Second
	}
	// Create queue handler chain.
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first.
	composedHandler := d.ProxyHandler

	composedHandler = requestAppMetricsHandler(logger, composedHandler, breaker, mp)
	composedHandler = queue.ProxyHandler(tracer, breaker, stats, composedHandler)
	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = handler.NewTimeoutHandler(composedHandler, "request timeout",
		func(r *http.Request) (time.Duration, time.Duration, time.Duration) {
			return timeout, responseStartTimeout, idleTimeout
		}, logger)

	composedHandler = queue.NewRouteTagHandler(composedHandler)
	composedHandler = withFullDuplex(composedHandler, env.EnableHTTPFullDuplex, logger)

	drainers.HijackedDrainer = &handler.HijackTracker{Handler: composedHandler}
	composedHandler = drainers.HijackedDrainer

	drainers.StandardDrainer = &pkghandler.Drainer{
		QuietPeriod: drainSleepDuration,
		// Add Activator probe header to the drainer so it can handle probes directly from activator
		HealthCheckUAPrefixes: []string{netheader.ActivatorUserAgent, netheader.AutoscalingUserAgent},
		Inner:                 composedHandler,
		HealthCheck:           health.ProbeHandler(tracer, prober),
	}

	composedHandler = drainers.StandardDrainer

	if env.Observability.EnableRequestLog {
		// We want to capture the probes/healthchecks in the request logs.
		// Hence we need to have RequestLogHandler be the first one.
		composedHandler = requestLogHandler(logger, composedHandler, env)
	}

	composedHandler = otelhttp.NewHandler(
		composedHandler,
		"queue",
		otelhttp.WithMeterProvider(mp),
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithFilter(func(r *http.Request) bool {
			return !netheader.IsProbe(r)
		}),
	)

	return composedHandler, drainers
}

func adminHandler(ctx context.Context, logger *zap.SugaredLogger, drainer *pkghandler.Drainer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(queue.RequestQueueDrainPath, func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Attached drain handler from user-container", r)

		go func() {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				// If the context isn't done then the queue proxy didn't
				// receive a TERM signal. Thus the user-container's
				// liveness probes are triggering the container to restart
				// and we shouldn't block that
				drainer.Reset()
			}
		}()

		drainer.Drain()
		w.WriteHeader(http.StatusOK)
	})

	return mux
}

func withFullDuplex(h http.Handler, enableFullDuplex bool, logger *zap.SugaredLogger) http.Handler {
	if !enableFullDuplex {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.EnableFullDuplex(); err != nil {
			logger.Errorw("Unable to enable full duplex", zap.Error(err))
		}
		h.ServeHTTP(w, r)
	})
}
