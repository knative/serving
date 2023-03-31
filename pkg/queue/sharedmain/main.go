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

package sharedmain

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.opencensus.io/plugin/ochttp"
	"go.uber.org/automaxprocs/maxprocs"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/types"

	"knative.dev/control-protocol/pkg/certificates"
	netstats "knative.dev/networking/pkg/http/stats"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/pkg/tracing/propagation/tracecontextb3"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/readiness"
)

const (
	// reportingPeriod is the interval of time between reporting stats by queue proxy.
	reportingPeriod = 1 * time.Second

	// Duration the /wait-for-drain handler should wait before returning.
	// This is to give networking a little bit more time to remove the pod
	// from its configuration and propagate that to all loadbalancers and nodes.
	drainSleepDuration = 30 * time.Second

	// certPath is the path for the server certificate mounted by queue-proxy.
	certPath = queue.CertDirectory + "/" + certificates.SecretCertKey

	// keyPath is the path for the server certificate key mounted by queue-proxy.
	keyPath = queue.CertDirectory + "/" + certificates.SecretPKKey

	// PodInfoAnnotationsPath is an exported path for the annotations file
	// This path is used by QP Options (Extensions).
	PodInfoAnnotationsPath = queue.PodInfoDirectory + "/" + queue.PodInfoAnnotationsFilename
)

type config struct {
	ContainerConcurrency                int    `split_words:"true" required:"true"`
	QueueServingPort                    string `split_words:"true" required:"true"`
	QueueServingTLSPort                 string `split_words:"true" required:"true"`
	UserPort                            string `split_words:"true" required:"true"`
	RevisionTimeoutSeconds              int    `split_words:"true" required:"true"`
	RevisionResponseStartTimeoutSeconds int    `split_words:"true"` // optional
	RevisionIdleTimeoutSeconds          int    `split_words:"true"` // optional
	ServingReadinessProbe               string `split_words:"true"` // optional
	EnableProfiling                     bool   `split_words:"true"` // optional
	EnableHTTP2AutoDetection            bool   `split_words:"true"` // optional

	// Logging configuration
	ServingLoggingConfig         string `split_words:"true" required:"true"`
	ServingLoggingLevel          string `split_words:"true" required:"true"`
	ServingRequestLogTemplate    string `split_words:"true"` // optional
	ServingEnableRequestLog      bool   `split_words:"true"` // optional
	ServingEnableProbeRequestLog bool   `split_words:"true"` // optional

	// Metrics configuration
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

	Env
}

// Env exposes parsed QP environment variables for use by Options (QP Extensions)
type Env struct {
	// ServingNamespace is the namespace in which the service is defined
	ServingNamespace string `split_words:"true" required:"true"`

	// ServingService is the name of the service served by this pod
	ServingService string `split_words:"true"` // optional

	// ServingConfiguration is the name of service configuration served by this pod
	ServingConfiguration string `split_words:"true" required:"true"`

	// ServingRevision is the name of service revision served by this pod
	ServingRevision string `split_words:"true" required:"true"`

	// ServingPod is the pod name
	ServingPod string `split_words:"true" required:"true"`

	// ServingPodIP is the pod ip address
	ServingPodIP string `split_words:"true" required:"true"`
}

// Defaults provides Options (QP Extensions) with the default bahaviour of QP
// Some attributes of Defaults may be modified by Options
// Modifying Defaults mutates the behavior of QP
type Defaults struct {
	// Logger enables Options to use the QP pre-configured logger
	// It is expected that Options will use the provided Logger when logging
	// Options should not modify the provided Default Logger
	Logger *zap.SugaredLogger

	// Env exposes parsed QP environment variables for use by Options
	// Options should not modify the provided environment parameters
	Env Env

	// Ctx provides Options with the QP context
	// An Option may derive a new context from Ctx. If a new context is derived,
	// the derived context should replace the value of Ctx.
	// The new Ctx will then be used by other Options (called next) and by QP.
	Ctx context.Context

	// Transport provides Options with the QP RoundTripper
	// An Option may wrap the provided Transport to add a Roundtripper.
	// If Transport is wrapped, the new RoundTripper should replace the value of Transport.
	// The new Transport will then be used by other Options (called next) and by QP.
	Transport http.RoundTripper
}

type Option func(*Defaults)

func init() {
	maxprocs.Set()
}

func Main(opts ...Option) error {
	d := Defaults{
		Ctx: signals.NewContext(),
	}

	// Parse the environment.
	var env config
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	d.Env = env.Env

	// Setup the Logger.
	logger, _ := pkglogging.NewLogger(env.ServingLoggingConfig, env.ServingLoggingLevel)
	defer flush(logger)

	logger = logger.Named("queueproxy").With(
		zap.String(logkey.Key, types.NamespacedName{
			Namespace: env.ServingNamespace,
			Name:      env.ServingRevision,
		}.String()),
		zap.String(logkey.Pod, env.ServingPod))

	d.Logger = logger
	d.Transport = buildTransport(env)

	if env.TracingConfigBackend != tracingconfig.None {
		oct := tracing.NewOpenCensusTracer(tracing.WithExporterFull(env.ServingPod, env.ServingPodIP, logger))
		oct.ApplyConfig(&tracingconfig.Config{
			Backend:        env.TracingConfigBackend,
			Debug:          env.TracingConfigDebug,
			ZipkinEndpoint: env.TracingConfigZipkinEndpoint,
			SampleRate:     env.TracingConfigSampleRate,
		})
		defer oct.Shutdown(context.Background())
	}

	// allow extensions to read d and return modified context and transport
	for _, opts := range opts {
		opts(&d)
	}

	// Report stats on Go memory usage every 30 seconds.
	metrics.MemStatsOrDie(d.Ctx)

	protoStatReporter := queue.NewProtobufStatsReporter(env.ServingPod, reportingPeriod)

	reportTicker := time.NewTicker(reportingPeriod)
	defer reportTicker.Stop()

	stats := netstats.NewRequestStats(time.Now())
	go func() {
		for now := range reportTicker.C {
			stat := stats.Report(now)
			protoStatReporter.Report(stat)
		}
	}()

	// Setup probe to run for checking user-application healthiness.
	// Do not set up probe if concurrency state endpoint is set, as
	// paused containers don't play well with k8s readiness probes.
	probe := func() bool { return true }
	if env.ServingReadinessProbe != "" && env.ConcurrencyStateEndpoint == "" {
		probe = buildProbe(logger, env.ServingReadinessProbe, env.EnableHTTP2AutoDetection).ProbeContainer
	}

	var concurrencyendpoint *queue.ConcurrencyEndpoint
	if env.ConcurrencyStateEndpoint != "" {
		concurrencyendpoint = queue.NewConcurrencyEndpoint(env.ConcurrencyStateEndpoint, env.ConcurrencyStateTokenPath)
	}

	// Enable TLS when certificate is mounted.
	tlsEnabled := exists(logger, certPath) && exists(logger, keyPath)

	mainHandler, drainer := mainHandler(d.Ctx, env, d.Transport, probe, stats, logger, concurrencyendpoint)
	adminHandler := adminHandler(d.Ctx, logger, drainer)

	// Enable TLS server when activator server certs are mounted.
	// At this moment activator with TLS does not disable HTTP.
	// See also https://github.com/knative/serving/issues/12808.
	httpServers := map[string]*http.Server{
		"main":    mainServer(":"+env.QueueServingPort, mainHandler),
		"admin":   adminServer(":"+strconv.Itoa(networking.QueueAdminPort), adminHandler),
		"metrics": metricsServer(protoStatReporter),
	}

	if env.EnableProfiling {
		httpServers["profile"] = profiling.NewServer(profiling.NewHandler(logger, true))
	}

	tlsServers := map[string]*http.Server{
		"main":  mainServer(":"+env.QueueServingTLSPort, mainHandler),
		"admin": adminServer(":"+strconv.Itoa(networking.QueueAdminPort), adminHandler),
	}

	if tlsEnabled {
		// Drop admin http server since the admin TLS server is listening on the same port
		delete(httpServers, "admin")
	} else {
		tlsServers = map[string]*http.Server{}
	}

	logger.Info("Starting queue-proxy")

	errCh := make(chan error)
	for name, server := range httpServers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			logger.Info("Starting http server ", name, s.Addr)
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("%s server failed to serve: %w", name, err)
			}
		}(name, server)
	}
	for name, server := range tlsServers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			logger.Info("Starting tls server ", name, s.Addr)
			if err := s.ListenAndServeTLS(certPath, keyPath); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
		return err
	case <-d.Ctx.Done():
		if env.ConcurrencyStateEndpoint != "" {
			concurrencyendpoint.Terminating(logger)
		}
		logger.Info("Received TERM signal, attempting to gracefully shutdown servers.")
		logger.Infof("Sleeping %v to allow K8s propagation of non-ready state", drainSleepDuration)
		drainer.Drain()

		for name, srv := range httpServers {
			logger.Info("Shutting down server: ", name)
			if err := srv.Shutdown(context.Background()); err != nil {
				logger.Errorw("Failed to shutdown server", zap.String("server", name), zap.Error(err))
			}
		}
		for name, srv := range tlsServers {
			logger.Info("Shutting down server: ", name)
			if err := srv.Shutdown(context.Background()); err != nil {
				logger.Errorw("Failed to shutdown server", zap.String("server", name), zap.Error(err))
			}
		}
		logger.Info("Shutdown complete, exiting...")
	}
	return nil
}

func exists(logger *zap.SugaredLogger, filename string) bool {
	_, err := os.Stat(filename)
	if err != nil && !os.IsNotExist(err) {
		logger.Fatalw(fmt.Sprintf("Failed to verify the file path %q", filename), zap.Error(err))
	}
	return err == nil
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

func buildTransport(env config) http.RoundTripper {
	maxIdleConns := 1000 // TODO: somewhat arbitrary value for CC=0, needs experimental validation.
	if env.ContainerConcurrency > 0 {
		maxIdleConns = env.ContainerConcurrency
	}
	// set max-idle and max-idle-per-host to same value since we're always proxying to the same host.
	transport := pkgnet.NewProxyAutoTransport(maxIdleConns /* max-idle */, maxIdleConns /* max-idle-per-host */)

	if env.TracingConfigBackend == tracingconfig.None {
		return transport
	}

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
