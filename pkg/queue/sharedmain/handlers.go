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
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	netheader "knative.dev/networking/pkg/http/header"
	netproxy "knative.dev/networking/pkg/http/proxy"
	netstats "knative.dev/networking/pkg/http/stats"
	pkghandler "knative.dev/pkg/network/handlers"
	"knative.dev/serving/pkg/activator"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/http/handler"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
)

func mainHandler(
	env config,
	transport http.RoundTripper,
	prober func() bool,
	stats *netstats.RequestStats,
	logger *zap.SugaredLogger,
	mp metric.MeterProvider,
	tp trace.TracerProvider,
	pendingRequests *atomic.Int32,
) (http.Handler, *pkghandler.Drainer) {
	target := net.JoinHostPort("127.0.0.1", env.UserPort)
	tracer := tp.Tracer("knative.dev/serving/pkg/queue")

	httpProxy := pkghttp.NewHeaderPruningReverseProxy(target, pkghttp.NoHostOverride, activator.RevisionHeaders, false /* use HTTP */)
	httpProxy.Transport = transport
	httpProxy.ErrorHandler = pkghandler.Error(logger)
	httpProxy.BufferPool = netproxy.NewBufferPool()
	httpProxy.FlushInterval = netproxy.FlushInterval

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
	var composedHandler http.Handler = httpProxy

	composedHandler = requestAppMetricsHandler(logger, composedHandler, breaker, mp)
	composedHandler = queue.ProxyHandler(tracer, breaker, stats, composedHandler)

	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = handler.NewTimeoutHandler(composedHandler, "request timeout", func(r *http.Request) (time.Duration, time.Duration, time.Duration) {
		return timeout, responseStartTimeout, idleTimeout
	})

	composedHandler = queue.NewRouteTagHandler(composedHandler)
	composedHandler = withFullDuplex(composedHandler, env.EnableHTTPFullDuplex, logger)

	drainer := &pkghandler.Drainer{
		QuietPeriod: drainSleepDuration,
		// Add Activator probe header to the drainer so it can handle probes directly from activator
		HealthCheckUAPrefixes: []string{netheader.ActivatorUserAgent, netheader.AutoscalingUserAgent},
		Inner:                 composedHandler,
		HealthCheck:           health.ProbeHandler(tracer, prober),
	}
	composedHandler = drainer

	composedHandler = withRequestCounter(composedHandler, pendingRequests)

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
	return composedHandler, drainer
}

func adminHandler(ctx context.Context, logger *zap.SugaredLogger, drainer *pkghandler.Drainer, pendingRequests *atomic.Int32) http.Handler {
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

	// New endpoint that returns 200 only when all requests are drained
	mux.HandleFunc("/drain-complete", func(w http.ResponseWriter, r *http.Request) {
		if pendingRequests.Load() <= 0 {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("drained"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "pending requests: %d", pendingRequests.Load())
		}
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

func isProbeRequest(r *http.Request) bool {
	// Check standard probes (K8s and Knative probe headers)
	if netheader.IsProbe(r) {
		return true
	}

	// Check all Knative internal probe user agents that should not be counted
	// as pending requests (matching what the Drainer filters)
	userAgent := r.Header.Get("User-Agent")
	return strings.HasPrefix(userAgent, netheader.ActivatorUserAgent) ||
		strings.HasPrefix(userAgent, netheader.AutoscalingUserAgent) ||
		strings.HasPrefix(userAgent, netheader.QueueProxyUserAgent) ||
		strings.HasPrefix(userAgent, netheader.IngressReadinessUserAgent)
}

func withRequestCounter(h http.Handler, pendingRequests *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only count non-probe requests as pending
		if !isProbeRequest(r) {
			pendingRequests.Add(1)
			defer pendingRequests.Add(-1)
		}
		h.ServeHTTP(w, r)
	})
}
