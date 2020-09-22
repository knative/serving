/*
Copyright 2018 The Knative Authors

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

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/types"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/pkg/tracing/propagation/tracecontextb3"
	activatorutil "knative.dev/serving/pkg/activator/util"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/http/handler"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
	"knative.dev/serving/pkg/queue/readiness"
)

const (
	badProbeTemplate = "unexpected probe header value: %s"

	// reportingPeriod is the interval of time between reporting stats by queue proxy.
	reportingPeriod = 1 * time.Second
)

var (
	readinessProbeTimeout = flag.Duration("probe-period", -1, "run readiness probe with given timeout")
	unixSocketPath        = filepath.Join(os.TempDir(), "queue.sock")
)

type config struct {
	ContainerConcurrency   int    `split_words:"true" required:"true"`
	QueueServingPort       int    `split_words:"true" required:"true"`
	UserPort               int    `split_words:"true" required:"true"`
	RevisionTimeoutSeconds int    `split_words:"true" required:"true"`
	ServingReadinessProbe  string `split_words:"true" required:"true"`
	EnableProfiling        bool   `split_words:"true"` // optional

	// Logging configuration
	ServingLoggingConfig         string `split_words:"true" required:"true"`
	ServingLoggingLevel          string `split_words:"true" required:"true"`
	ServingRequestLogTemplate    string `split_words:"true"` // optional
	ServingEnableRequestLog      bool   `split_words:"true"` // optional
	ServingEnableProbeRequestLog bool   `split_words:"true"` // optional

	// Metrics configuration
	ServingNamespace             string `split_words:"true" required:"true"`
	ServingRevision              string `split_words:"true" required:"true"`
	ServingConfiguration         string `split_words:"true" required:"true"`
	ServingPodIP                 string `split_words:"true" required:"true"`
	ServingPod                   string `split_words:"true" required:"true"`
	ServingService               string `split_words:"true"` // optional
	ServingRequestMetricsBackend string `split_words:"true"` // optional
	MetricsCollectorAddress      string `split_words:"true"` // optional

	// Tracing configuration
	TracingConfigDebug                bool                      `split_words:"true"` // optional
	TracingConfigBackend              tracingconfig.BackendType `split_words:"true"` // optional
	TracingConfigSampleRate           float64                   `split_words:"true"` // optional
	TracingConfigZipkinEndpoint       string                    `split_words:"true"` // optional
	TracingConfigStackdriverProjectID string                    `split_words:"true"` // optional
}

func knativeProbeHandler(logger *zap.SugaredLogger, healthState *health.State, prober func() bool, isAggressive bool, tracingEnabled bool, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ph := network.KnativeProbeHeader(r)

		if ph == "" {
			next.ServeHTTP(w, r)
			return
		}

		var probeSpan *trace.Span
		if tracingEnabled {
			_, probeSpan = trace.StartSpan(r.Context(), "probe")
			defer probeSpan.End()
		}

		if ph != queue.Name {
			http.Error(w, fmt.Sprintf(badProbeTemplate, ph), http.StatusBadRequest)
			probeSpan.Annotate([]trace.Attribute{
				trace.StringAttribute("queueproxy.probe.error", fmt.Sprintf(badProbeTemplate, ph))}, "error")
			return
		}

		if prober == nil {
			http.Error(w, "no probe", http.StatusInternalServerError)
			probeSpan.Annotate([]trace.Attribute{
				trace.StringAttribute("queueproxy.probe.error", "no probe")}, "error")
			return
		}

		healthState.HandleHealthProbe(func() bool {
			if !prober() {
				probeSpan.Annotate([]trace.Attribute{
					trace.StringAttribute("queueproxy.probe.error", "container not ready")}, "error")
				return false
			}
			return true
		}, isAggressive, w)
	}
}

func main() {
	flag.Parse()
	ctx := signals.NewContext()

	// If this is set, we run as a standalone binary to probe the queue-proxy.
	if *readinessProbeTimeout >= 0 {
		// Use a unix socket rather than TCP to avoid going via entire TCP stack
		// when we're actually in the same container.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", unixSocketPath)
		}

		os.Exit(standaloneProbeMain(*readinessProbeTimeout, transport))
	}

	// Parse the environment.
	var env config
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Setup the logger.
	logger, _ := pkglogging.NewLogger(env.ServingLoggingConfig, env.ServingLoggingLevel)
	logger = logger.Named("queueproxy")
	defer flush(logger)

	logger = logger.With(
		zap.Object(logkey.Key, pkglogging.NamespacedName(types.NamespacedName{
			Namespace: env.ServingNamespace,
			Name:      env.ServingRevision,
		})),
		zap.String(logkey.Pod, env.ServingPod))

	// Report stats on Go memory usage every 30 seconds.
	msp := metrics.NewMemStatsAll()
	msp.Start(context.Background(), 30*time.Second)
	if err := view.Register(msp.DefaultViews()...); err != nil {
		logger.Fatalw("Error exporting go memstats view", zap.Error(err))
	}

	// Setup reporters and processes to handle stat reporting.
	promStatReporter, err := queue.NewPrometheusStatsReporter(
		env.ServingNamespace, env.ServingConfiguration, env.ServingRevision,
		env.ServingPod, reportingPeriod)
	if err != nil {
		logger.Fatalw("Failed to create stats reporter", zap.Error(err))
	}

	protoStatReporter := queue.NewProtobufStatsReporter(env.ServingPod, reportingPeriod)

	reportTicker := time.NewTicker(reportingPeriod)
	defer reportTicker.Stop()

	stats := network.NewRequestStats(time.Now())
	go func() {
		for now := range reportTicker.C {
			stat := stats.Report(now)
			promStatReporter.Report(stat)
			protoStatReporter.Report(stat)
		}
	}()

	// Setup probe to run for checking user-application healthiness.
	probe := buildProbe(logger, env.ServingReadinessProbe)
	healthState := &health.State{}

	server := buildServer(ctx, env, healthState, probe, stats, logger)
	adminServer := buildAdminServer(logger, healthState)
	metricsServer := buildMetricsServer(promStatReporter, protoStatReporter)

	servers := map[string]*http.Server{
		"main":    server,
		"admin":   adminServer,
		"metrics": metricsServer,
	}

	if env.EnableProfiling {
		servers["profile"] = profiling.NewServer(profiling.NewHandler(logger, true))
	}

	errCh := make(chan error, len(servers)+1)
	listeners := make(map[string]net.Listener, len(servers))
	for name, server := range servers {
		l, err := net.Listen("tcp", server.Addr)
		if err != nil {
			logger.Fatalw("listen failed", zap.Error(err))
		}

		listeners[name] = l
	}

	for name, server := range servers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			if err := s.Serve(listeners[name]); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("%s server failed: %w", name, err)
			}
		}(name, server)
	}

	// Listen on a unix socket so that the exec probe can avoid having to go
	// through the full tcp network stack.
	go func() {
		l, err := net.Listen("unix", unixSocketPath)
		if err != nil {
			errCh <- err
			return
		}
		if err := http.Serve(l, server.Handler); err != nil {
			errCh <- err
		}
	}()

	// Blocks until we actually receive a TERM signal or one of the servers
	// exit unexpectedly. We fold both signals together because we only want
	// to act on the first of those to reach here.
	select {
	case err := <-errCh:
		logger.Errorw("Failed to bring up queue-proxy, shutting down.", zap.Error(err))
		// This extra flush is needed because defers are not handled via os.Exit calls.
		flush(logger)
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("Received TERM signal, attempting to gracefully shutdown servers.")
		healthState.Shutdown(func() {
			logger.Infof("Sleeping %v to allow K8s propagation of non-ready state", pkgnet.DefaultDrainTimeout)
			time.Sleep(pkgnet.DefaultDrainTimeout)

			// Calling server.Shutdown() allows pending requests to
			// complete, while no new work is accepted.
			logger.Info("Shutting down main server")
			if err := server.Shutdown(context.Background()); err != nil {
				logger.Errorw("Failed to shutdown proxy server", zap.Error(err))
			}
			// Removing the main server from the shutdown logic as we've already shut it down.
			delete(servers, "main")
		})

		for serverName, srv := range servers {
			logger.Info("Shutting down server: ", serverName)
			if err := srv.Shutdown(context.Background()); err != nil {
				logger.Errorw("Failed to shutdown server", zap.String("server", serverName), zap.Error(err))
			}
		}
		logger.Info("Shutdown complete, exiting...")
	}
}

func buildProbe(logger *zap.SugaredLogger, probeJSON string) *readiness.Probe {
	coreProbe, err := readiness.DecodeProbe(probeJSON)
	if err != nil {
		logger.Fatalw("Queue container failed to parse readiness probe", zap.Error(err))
	}
	return readiness.NewProbe(coreProbe)
}

func buildServer(ctx context.Context, env config, healthState *health.State, rp *readiness.Probe, stats *network.RequestStats,
	logger *zap.SugaredLogger) *http.Server {
	target := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(env.UserPort)),
	}

	maxIdleConns := 1000 // TODO: somewhat arbitrary value for CC=0, needs experimental validation.
	if env.ContainerConcurrency > 0 {
		maxIdleConns = env.ContainerConcurrency
	}

	httpProxy := httputil.NewSingleHostReverseProxy(target)
	httpProxy.Transport = buildTransport(env, logger, maxIdleConns)
	httpProxy.ErrorHandler = pkgnet.ErrorHandler(logger)
	httpProxy.BufferPool = network.NewBufferPool()
	httpProxy.FlushInterval = network.FlushInterval
	activatorutil.SetupHeaderPruning(httpProxy)

	breaker := buildBreaker(logger, env)
	metricsSupported := supportsMetrics(ctx, logger, env)
	tracingEnabled := env.TracingConfigBackend != tracingconfig.None
	timeout := time.Duration(env.RevisionTimeoutSeconds) * time.Second

	// Create queue handler chain.
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first.
	var composedHandler http.Handler = httpProxy
	if metricsSupported {
		composedHandler = requestAppMetricsHandler(logger, composedHandler, breaker, env)
	}
	composedHandler = queue.ProxyHandler(breaker, stats, tracingEnabled, composedHandler)
	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = handler.NewTimeToFirstByteTimeoutHandler(composedHandler, "request timeout", handler.StaticTimeoutFunc(timeout))

	if metricsSupported {
		composedHandler = requestMetricsHandler(logger, composedHandler, env)
	}
	composedHandler = tracing.HTTPSpanMiddleware(composedHandler)

	composedHandler = knativeProbeHandler(logger, healthState, rp.ProbeContainer, rp.IsAggressive(), tracingEnabled, composedHandler)
	composedHandler = network.NewProbeHandler(composedHandler)
	// We might want sometimes capture the probes/healthchecks in the request
	// logs. Hence we need to have RequestLogHandler to be the first one.
	composedHandler = pushRequestLogHandler(logger, composedHandler, env)

	return pkgnet.NewServer(":"+strconv.Itoa(env.QueueServingPort), composedHandler)
}

func buildTransport(env config, logger *zap.SugaredLogger, maxConns int) http.RoundTripper {
	// set max-idle and max-idle-per-host to same value since we're always proxying to the same host.
	transport := pkgnet.NewAutoTransport(maxConns /* max-idle */, maxConns /* max-idle-per-host */)

	if env.TracingConfigBackend == tracingconfig.None {
		return transport
	}

	oct := tracing.NewOpenCensusTracer(tracing.WithExporterFull(env.ServingPod, env.ServingPodIP, logger))
	oct.ApplyConfig(&tracingconfig.Config{
		Backend:              env.TracingConfigBackend,
		Debug:                env.TracingConfigDebug,
		ZipkinEndpoint:       env.TracingConfigZipkinEndpoint,
		StackdriverProjectID: env.TracingConfigStackdriverProjectID,
		SampleRate:           env.TracingConfigSampleRate,
	})

	return &ochttp.Transport{
		Base:        transport,
		Propagation: tracecontextb3.B3Egress,
	}
}

func buildBreaker(logger *zap.SugaredLogger, env config) *queue.Breaker {
	if env.ContainerConcurrency < 1 {
		return nil
	}

	// We set the queue depth to be equal to the container concurrency * 10 to
	// allow the autoscaler time to react.
	queueDepth := env.ContainerConcurrency * 10
	params := queue.BreakerParams{QueueDepth: queueDepth, MaxConcurrency: env.ContainerConcurrency, InitialCapacity: env.ContainerConcurrency}
	logger.Infof("Queue container is starting with %#v", params)

	return queue.NewBreaker(params)
}

func supportsMetrics(ctx context.Context, logger *zap.SugaredLogger, env config) bool {
	// Setup request metrics reporting for end-user metrics.
	if env.ServingRequestMetricsBackend == "" {
		return false
	}

	if err := setupMetricsExporter(ctx, logger, env.ServingRequestMetricsBackend, env.MetricsCollectorAddress); err != nil {
		logger.Errorw("Error setting up request metrics exporter. Request metrics will be unavailable.", zap.Error(err))
		return false
	}

	return true
}

func buildAdminServer(logger *zap.SugaredLogger, healthState *health.State) *http.Server {
	adminMux := http.NewServeMux()
	drainHandler := healthState.DrainHandlerFunc()
	adminMux.HandleFunc(queue.RequestQueueDrainPath, func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Attached drain handler from user-container")
		drainHandler(w, r)
	})

	return &http.Server{
		Addr:    ":" + strconv.Itoa(networking.QueueAdminPort),
		Handler: adminMux,
	}
}

func buildMetricsServer(promStatReporter *queue.PrometheusStatsReporter, protobufStatReporter *queue.ProtobufStatsReporter) *http.Server {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", queue.NewStatsHandler(promStatReporter, protobufStatReporter))
	return &http.Server{
		Addr:    ":" + strconv.Itoa(networking.AutoscalingQueueMetricsPort),
		Handler: metricsMux,
	}
}

func pushRequestLogHandler(logger *zap.SugaredLogger, currentHandler http.Handler, env config) http.Handler {
	if !env.ServingEnableRequestLog {
		return currentHandler
	}

	revInfo := &pkghttp.RequestLogRevision{
		Name:          env.ServingRevision,
		Namespace:     env.ServingNamespace,
		Service:       env.ServingService,
		Configuration: env.ServingConfiguration,
		PodName:       env.ServingPod,
		PodIP:         env.ServingPodIP,
	}
	handler, err := pkghttp.NewRequestLogHandler(currentHandler, logging.NewSyncFileWriter(os.Stdout), env.ServingRequestLogTemplate,
		pkghttp.RequestLogTemplateInputGetterFromRevision(revInfo), env.ServingEnableProbeRequestLog)
	if err != nil {
		logger.Errorw("Error setting up request logger. Request logs will be unavailable.", zap.Error(err))
		return currentHandler
	}
	return handler
}

func requestMetricsHandler(logger *zap.SugaredLogger, currentHandler http.Handler, env config) http.Handler {
	h, err := queue.NewRequestMetricsHandler(currentHandler, env.ServingNamespace,
		env.ServingService, env.ServingConfiguration, env.ServingRevision, env.ServingPod)
	if err != nil {
		logger.Errorw("Error setting up request metrics reporter. Request metrics will be unavailable.", zap.Error(err))
		return currentHandler
	}
	return h
}

func requestAppMetricsHandler(logger *zap.SugaredLogger, currentHandler http.Handler, breaker *queue.Breaker, env config) http.Handler {
	h, err := queue.NewAppRequestMetricsHandler(currentHandler, breaker, env.ServingNamespace,
		env.ServingService, env.ServingConfiguration, env.ServingRevision, env.ServingPod)
	if err != nil {
		logger.Errorw("Error setting up app request metrics reporter. Request metrics will be unavailable.", zap.Error(err))
		return currentHandler
	}
	return h
}

func setupMetricsExporter(ctx context.Context, logger *zap.SugaredLogger, backend string, collectorAddress string) error {
	// Set up OpenCensus exporter.
	// NOTE: We use revision as the component instead of queue because queue is
	// implementation specific. The current metrics are request relative. Using
	// revision is reasonable.
	// TODO(yanweiguo): add the ability to emit metrics with names not combined
	// to component.
	ops := metrics.ExporterOptions{
		Domain:         metrics.Domain(),
		Component:      "revision",
		PrometheusPort: networking.UserQueueMetricsPort,
		ConfigMap: map[string]string{
			metrics.BackendDestinationKey: backend,
			"metrics.opencensus-address":  collectorAddress,
		},
	}
	return metrics.UpdateExporter(ctx, ops, logger)
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	os.Stdout.Sync()
	os.Stderr.Sync()
	metrics.FlushExporter()
}
