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
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	netheader "knative.dev/networking/pkg/http/header"
	netproxy "knative.dev/networking/pkg/http/proxy"
	netstats "knative.dev/networking/pkg/http/stats"
	pkghandler "knative.dev/pkg/network/handlers"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/activator"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/http/handler"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
)

func mainHandler(
	ctx context.Context,
	env config,
	transport http.RoundTripper,
	prober func() bool,
	stats *netstats.RequestStats,
	logger *zap.SugaredLogger,
	ce *queue.ConcurrencyEndpoint,
) (http.Handler, *pkghandler.Drainer) {
	target := net.JoinHostPort("127.0.0.1", env.UserPort)

	httpProxy := pkghttp.NewHeaderPruningReverseProxy(target, pkghttp.NoHostOverride, activator.RevisionHeaders, false /* use HTTP */)
	httpProxy.Transport = transport
	httpProxy.ErrorHandler = pkghandler.Error(logger)
	httpProxy.BufferPool = netproxy.NewBufferPool()
	httpProxy.FlushInterval = netproxy.FlushInterval

	breaker := buildBreaker(logger, env)
	tracingEnabled := env.TracingConfigBackend != tracingconfig.None
	concurrencyStateEnabled := env.ConcurrencyStateEndpoint != ""
	timeout := time.Duration(env.RevisionTimeoutSeconds) * time.Second
	var responseStartTimeout = 0 * time.Second
	if env.RevisionResponseStartTimeoutSeconds != 0 {
		responseStartTimeout = time.Duration(env.RevisionResponseStartTimeoutSeconds) * time.Second
	}
	var idleTimeout = 0 * time.Second
	if env.RevisionIdleTimeoutSeconds != 0 {
		idleTimeout = time.Duration(env.RevisionIdleTimeoutSeconds) * time.Second
	}
	// Create queue handler chain.
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first.
	var composedHandler http.Handler = httpProxy
	if concurrencyStateEnabled {
		logger.Info("Concurrency state endpoint set, tracking request counts, using endpoint: ", ce.Endpoint())
		go func() {
			for range time.NewTicker(1 * time.Minute).C {
				ce.RefreshToken()
			}
		}()
		composedHandler = queue.ConcurrencyStateHandler(logger, composedHandler, ce.Pause, ce.Resume)
		// start paused
		ce.Pause(logger)
	}

	metricsSupported := supportsMetrics(ctx, logger, env)
	if metricsSupported {
		composedHandler = requestAppMetricsHandler(logger, composedHandler, breaker, env)
	}
	composedHandler = queue.ProxyHandler(breaker, stats, tracingEnabled, composedHandler)
	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = handler.NewTimeoutHandler(composedHandler, "request timeout", func(r *http.Request) (time.Duration, time.Duration, time.Duration) {
		return timeout, responseStartTimeout, idleTimeout
	})

	if metricsSupported {
		composedHandler = requestMetricsHandler(logger, composedHandler, env)
	}
	if tracingEnabled {
		composedHandler = tracing.HTTPSpanMiddleware(composedHandler)
	}

	drainer := &pkghandler.Drainer{
		QuietPeriod: drainSleepDuration,
		// Add Activator probe header to the drainer so it can handle probes directly from activator
		HealthCheckUAPrefixes: []string{netheader.ActivatorUserAgent, netheader.AutoscalingUserAgent},
		Inner:                 composedHandler,
		HealthCheck:           health.ProbeHandler(prober, tracingEnabled),
	}
	composedHandler = drainer

	if env.ServingEnableRequestLog {
		// We want to capture the probes/healthchecks in the request logs.
		// Hence we need to have RequestLogHandler be the first one.
		composedHandler = requestLogHandler(logger, composedHandler, env)
	}
	return composedHandler, drainer
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
