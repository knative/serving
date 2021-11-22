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
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.opencensus.io/plugin/ochttp"
	"go.uber.org/automaxprocs/maxprocs"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/types"

	network "knative.dev/networking/pkg"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	pkgnet "knative.dev/pkg/network"
	pkghandler "knative.dev/pkg/network/handlers"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/pkg/tracing/propagation/tracecontextb3"
	"knative.dev/serving/pkg/activator"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/http/handler"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
	"knative.dev/serving/pkg/queue/readiness"
)

const (
	// reportingPeriod is the interval of time between reporting stats by queue proxy.
	reportingPeriod = 1 * time.Second

	// Duration the /wait-for-drain handler should wait before returning.
	// This is to give networking a little bit more time to remove the pod
	// from its configuration and propagate that to all loadbalancers and nodes.
	drainSleepDuration = 30 * time.Second
)

type config struct {
	ContainerConcurrency     int    `split_words:"true" required:"true"`
	QueueServingPort         string `split_words:"true" required:"true"`
	UserPort                 string `split_words:"true" required:"true"`
	RevisionTimeoutSeconds   int    `split_words:"true" required:"true"`
	ServingReadinessProbe    string `split_words:"true"` // optional
	EnableProfiling          bool   `split_words:"true"` // optional
	EnableHTTP2AutoDetection bool   `split_words:"true"` // optional

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
	TracingConfigDebug          bool                      `split_words:"true"` // optional
	TracingConfigBackend        tracingconfig.BackendType `split_words:"true"` // optional
	TracingConfigSampleRate     float64                   `split_words:"true"` // optional
	TracingConfigZipkinEndpoint string                    `split_words:"true"` // optional

	// Concurrency State Endpoint configuration
	ConcurrencyStateEndpoint  string `split_words:"true"` // optional
	ConcurrencyStateTokenPath string `split_words:"true"` // optional
}

func init() {
	maxprocs.Set()
}

func main() {
	ctx := signals.NewContext()

	// Parse the environment.
	var env config
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Setup the logger.
	logger, _ := pkglogging.NewLogger(env.ServingLoggingConfig, env.ServingLoggingLevel)
	defer flush(logger)

	logger = logger.Named("queueproxy").With(
		zap.String(logkey.Key, types.NamespacedName{
			Namespace: env.ServingNamespace,
			Name:      env.ServingRevision,
		}.String()),
		zap.String(logkey.Pod, env.ServingPod))

	// Report stats on Go memory usage every 30 seconds.
	metrics.MemStatsOrDie(ctx)

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
	probe := func() bool { return true }
	if env.ServingReadinessProbe != "" {
		probe = buildProbe(logger, env.ServingReadinessProbe, env.EnableHTTP2AutoDetection).ProbeContainer
	}

	var concurrencyendpoint *queue.ConcurrencyEndpoint
	if env.ConcurrencyStateEndpoint != "" {
		concurrencyendpoint = queue.NewConcurrencyEndpoint(env.ConcurrencyStateEndpoint, env.ConcurrencyStateTokenPath)
	}
	mainServer, drain := buildServer(ctx, env, probe, stats, logger, concurrencyendpoint)
	servers := map[string]*http.Server{
		"main":    mainServer,
		"admin":   buildAdminServer(logger, drain),
		"metrics": buildMetricsServer(promStatReporter, protoStatReporter),
	}
	if env.EnableProfiling {
		servers["profile"] = profiling.NewServer(profiling.NewHandler(logger, true))
	}

	errCh := make(chan error)
	for name, server := range servers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("%s server failed to serve: %w", name, err)
			}
		}(name, server)
	}

	// Blocks until we actually receive a TERM signal or one of the servers
	// exits unexpectedly. We fold both signals together because we only want
	// to act on the first of those to reach here.
	select {
	case err := <-errCh:
		logger.Errorw("Failed to bring up queue-proxy, shutting down.", zap.Error(err))
		// This extra flush is needed because defers are not handled via os.Exit calls.
		flush(logger)
		os.Exit(1)
	case <-ctx.Done():
		if env.ConcurrencyStateEndpoint != "" {
			concurrencyendpoint.Terminating(logger)
		}
		logger.Info("Received TERM signal, attempting to gracefully shutdown servers.")
		logger.Infof("Sleeping %v to allow K8s propagation of non-ready state", drainSleepDuration)
		drain()

		// Removing the main server from the shutdown logic as we've already shut it down.
		delete(servers, "main")

		for serverName, srv := range servers {
			logger.Info("Shutting down server: ", serverName)
			if err := srv.Shutdown(context.Background()); err != nil {
				logger.Errorw("Failed to shutdown server", zap.String("server", serverName), zap.Error(err))
			}
		}
		logger.Info("Shutdown complete, exiting...")
	}
}

func buildProbe(logger *zap.SugaredLogger, encodedProbe string, autodetectHTTP2 bool) *readiness.Probe {
	coreProbe, err := readiness.DecodeProbe(encodedProbe)
	if err != nil {
		logger.Fatalw("Queue container failed to parse readiness probe", zap.Error(err))
	}
	if autodetectHTTP2 {
		return readiness.NewProbeWithHTTP2AutoDetection(coreProbe)
	}
	return readiness.NewProbe(coreProbe)
}

func buildServer(ctx context.Context, env config, probeContainer func() bool, stats *network.RequestStats, logger *zap.SugaredLogger,
	ce *queue.ConcurrencyEndpoint) (server *http.Server, drain func()) {
	maxIdleConns := 1000 // TODO: somewhat arbitrary value for CC=0, needs experimental validation.
	if env.ContainerConcurrency > 0 {
		maxIdleConns = env.ContainerConcurrency
	}

	target := net.JoinHostPort("127.0.0.1", env.UserPort)

	httpProxy := pkghttp.NewHeaderPruningReverseProxy(target, pkghttp.NoHostOverride, activator.RevisionHeaders)
	httpProxy.Transport = buildTransport(env, logger, maxIdleConns)
	httpProxy.ErrorHandler = pkghandler.Error(logger)
	httpProxy.BufferPool = network.NewBufferPool()
	httpProxy.FlushInterval = network.FlushInterval

	breaker := buildBreaker(logger, env)
	metricsSupported := supportsMetrics(ctx, logger, env)
	tracingEnabled := env.TracingConfigBackend != tracingconfig.None
	concurrencyStateEnabled := env.ConcurrencyStateEndpoint != ""
	firstByteTimeout := time.Duration(env.RevisionTimeoutSeconds) * time.Second
	// hardcoded to always disable idle timeout for now, will expose this later
	var idleTimeout time.Duration

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
	if metricsSupported {
		composedHandler = requestAppMetricsHandler(logger, composedHandler, breaker, env)
	}
	composedHandler = queue.ProxyHandler(breaker, stats, tracingEnabled, composedHandler)
	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = handler.NewTimeoutHandler(composedHandler, "request timeout", firstByteTimeout, idleTimeout)

	if metricsSupported {
		composedHandler = requestMetricsHandler(logger, composedHandler, env)
	}
	if tracingEnabled {
		composedHandler = tracing.HTTPSpanMiddleware(composedHandler)
	}

	drainer := &pkghandler.Drainer{
		QuietPeriod: drainSleepDuration,
		// Add Activator probe header to the drainer so it can handle probes directly from activator
		HealthCheckUAPrefixes: []string{network.ActivatorUserAgent},
		Inner:                 composedHandler,
		HealthCheck:           health.ProbeHandler(probeContainer, tracingEnabled),
	}
	composedHandler = drainer

	if env.ServingEnableRequestLog {
		// We want to capture the probes/healthchecks in the request logs.
		// Hence we need to have RequestLogHandler be the first one.
		composedHandler = requestLogHandler(logger, composedHandler, env)
	}

	return pkgnet.NewServer(":"+env.QueueServingPort, composedHandler), drainer.Drain
}

func buildTransport(env config, logger *zap.SugaredLogger, maxConns int) http.RoundTripper {
	// set max-idle and max-idle-per-host to same value since we're always proxying to the same host.
	transport := pkgnet.NewProxyAutoTransport(maxConns /* max-idle */, maxConns /* max-idle-per-host */)

	if env.TracingConfigBackend == tracingconfig.None {
		return transport
	}

	oct := tracing.NewOpenCensusTracer(tracing.WithExporterFull(env.ServingPod, env.ServingPodIP, logger))
	oct.ApplyConfig(&tracingconfig.Config{
		Backend:        env.TracingConfigBackend,
		Debug:          env.TracingConfigDebug,
		ZipkinEndpoint: env.TracingConfigZipkinEndpoint,
		SampleRate:     env.TracingConfigSampleRate,
	})

	return &ochttp.Transport{
		Base:        transport,
		Propagation: tracecontextb3.TraceContextB3Egress,
	}
}

func buildBreaker(logger *zap.SugaredLogger, env config) *queue.Breaker {
	if env.ContainerConcurrency < 1 {
		return nil
	}

	// We set the queue depth to be equal to the container concurrency * 10 to
	// allow the autoscaler time to react.
	queueDepth := 10 * env.ContainerConcurrency
	params := queue.BreakerParams{
		QueueDepth:      queueDepth,
		MaxConcurrency:  env.ContainerConcurrency,
		InitialCapacity: env.ContainerConcurrency,
	}
	logger.Infof("Queue container is starting with BreakerParams = %#v", params)
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

func buildAdminServer(logger *zap.SugaredLogger, drain func()) *http.Server {
	adminMux := http.NewServeMux()
	adminMux.HandleFunc(queue.RequestQueueDrainPath, func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Attached drain handler from user-container")
		drain()
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

func requestLogHandler(logger *zap.SugaredLogger, currentHandler http.Handler, env config) http.Handler {
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
