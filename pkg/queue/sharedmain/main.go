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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"

	netproxy "knative.dev/networking/pkg/http/proxy"
	pkghandler "knative.dev/pkg/network/handlers"
	"knative.dev/serving/pkg/activator"

	"github.com/kelseyhightower/envconfig"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/automaxprocs/maxprocs"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/networking/pkg/certificates"
	netstats "knative.dev/networking/pkg/http/stats"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/observability/runtime"
	"knative.dev/pkg/signals"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/observability"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/certificate"
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
	certPath = queue.CertDirectory + "/" + certificates.CertName

	// keyPath is the path for the server certificate key mounted by queue-proxy.
	keyPath = queue.CertDirectory + "/" + certificates.PrivateKeyName

	// PodInfoAnnotationsPath is an exported path for the annotations file
	// This path is used by QP Options (Extensions).
	PodInfoAnnotationsPath = queue.PodInfoDirectory + "/" + queue.PodInfoAnnotationsFilename

	// QPOptionTokenDirPath is a directory for per audience tokens
	// This path is used by QP Options (Extensions) as <QPOptionTokenDirPath>/<Audience>
	QPOptionTokenDirPath = queue.TokenDirectory
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

	// See https://github.com/knative/serving/issues/12387
	EnableHTTPFullDuplex       bool `split_words:"true"`                      // optional
	EnableHTTP2AutoDetection   bool `envconfig:"ENABLE_HTTP2_AUTO_DETECTION"` // optional
	EnableMultiContainerProbes bool `split_words:"true"`

	// Logging configuration
	ServingLoggingConfig string `split_words:"true" required:"true"`
	ServingLoggingLevel  string `split_words:"true" required:"true"`

	// Metrics, Tracing and Profiling
	Observability observability.Config `ignored:"true"`

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

	// ProxyHandler provides Options with the QP's main ReverseProxy
	// The default value is of type *httputil.ReverseProxy
	// An Option may type assert to *httutil.ReverseProxy and adjust attributes of the ReverseProxy,
	// or replace the handler entirely, typically to insert additional handler(s) into the chain.
	ProxyHandler http.Handler
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
	env := config{
		Observability: *observability.DefaultConfig(),
	}

	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	d.Env = env.Env

	// Setup the Logger.
	logger, _ := pkglogging.NewLogger(env.ServingLoggingConfig, env.ServingLoggingLevel)
	defer flush(logger)

	if data := os.Getenv("OBSERVABILITY_CONFIG"); data != "" {
		err := json.Unmarshal([]byte(data), &env.Observability)
		if err != nil {
			logger.Fatal("failed to parse OBSERVABILITY_CONFIG", zap.Error(err))
		}
	}

	logger = logger.Named("queueproxy").With(
		zap.String(logkey.Key, types.NamespacedName{
			Namespace: env.ServingNamespace,
			Name:      env.ServingRevision,
		}.String()),
		zap.String(logkey.Pod, env.ServingPod))

	d.Logger = logger

	mp, tp := SetupObservabilityOrDie(d.Ctx, &env, logger)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := mp.Shutdown(ctx); err != nil {
			logger.Errorw("Error flushing metrics", zap.Error(err))
		}
		if err := tp.Shutdown(ctx); err != nil {
			logger.Errorw("Error flushing traces", zap.Error(err))
		}
	}()

	d.Transport = buildTransport(env, tp, mp)
	proxyHandler := buildProxyHandler(logger, env, d.Transport)

	applyOptions(&d, proxyHandler, opts...)

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
	probe := func() bool { return true }
	if env.ServingReadinessProbe != "" {
		probe = buildProbe(logger, env.ServingReadinessProbe, env.EnableHTTP2AutoDetection, env.EnableMultiContainerProbes).ProbeContainer
	}

	// Enable TLS when certificate is mounted.
	tlsEnabled := exists(logger, certPath) && exists(logger, keyPath)

	mainHandler, drainer := mainHandler(env, d, probe, stats, logger, mp, tp)
	adminHandler := adminHandler(d.Ctx, logger, drainer)

	// Enable TLS server when activator server certs are mounted.
	// At this moment activator with TLS does not disable HTTP.
	// See also https://github.com/knative/serving/issues/12808.
	httpServers := map[string]*http.Server{
		"main":    mainServer(":"+env.QueueServingPort, mainHandler),
		"admin":   adminServer(":"+strconv.Itoa(networking.QueueAdminPort), adminHandler),
		"metrics": metricsServer(protoStatReporter),
	}

	if env.Observability.Runtime.ProfilingEnabled() {
		logger.Info("Rutime profiling enabled")
		pprof := runtime.NewProfilingServer()
		pprof.SetEnabled(true)
		httpServers["profile"] = pprof.Server
	}

	tlsServers := make(map[string]*http.Server)
	var certWatcher *certificate.CertWatcher
	var err error

	if tlsEnabled {
		tlsServers["main"] = mainServer(":"+env.QueueServingTLSPort, mainHandler)
		tlsServers["admin"] = adminServer(":"+strconv.Itoa(networking.QueueAdminPort), adminHandler)

		certWatcher, err = certificate.NewCertWatcher(certPath, keyPath, 1*time.Minute, logger)
		if err != nil {
			logger.Fatal("failed to create certWatcher", zap.Error(err))
		}
		defer certWatcher.Stop()

		// Drop admin http server since the admin TLS server is listening on the same port
		delete(httpServers, "admin")
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
			logger.Info("Starting tls server ", name, s.Addr)
			s.TLSConfig = &tls.Config{
				GetCertificate: certWatcher.GetCertificate,
				MinVersion:     tls.VersionTLS13,
			}
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			if err := s.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

func applyOptions(d *Defaults, proxyHandler *httputil.ReverseProxy, opts ...Option) {
	d.ProxyHandler = proxyHandler

	// allow extensions to read d and return modified context, transport, and proxy handler.
	for _, opts := range opts {
		opts(d)
	}

	proxyHandler.Transport = d.Transport
}

func exists(logger *zap.SugaredLogger, filename string) bool {
	_, err := os.Stat(filename)
	if err != nil && !os.IsNotExist(err) {
		logger.Fatalw(fmt.Sprintf("Failed to verify the file path %q", filename), zap.Error(err))
	}
	return err == nil
}

func buildProbe(logger *zap.SugaredLogger, encodedProbe string, autodetectHTTP2 bool, multiContainerProbes bool) *readiness.Probe {
	coreProbes, err := readiness.DecodeProbes(encodedProbe, multiContainerProbes)
	if err != nil {
		logger.Fatalw("Queue container failed to parse readiness probe", zap.Error(err))
	}
	if autodetectHTTP2 {
		return readiness.NewProbeWithHTTP2AutoDetection(coreProbes)
	}
	return readiness.NewProbe(coreProbes)
}

func buildTransport(env config, tp trace.TracerProvider, mp metric.MeterProvider) http.RoundTripper {
	maxIdleConns := 1000 // TODO: somewhat arbitrary value for CC=0, needs experimental validation.
	if env.ContainerConcurrency > 0 {
		maxIdleConns = env.ContainerConcurrency
	}
	// set max-idle and max-idle-per-host to same value since we're always proxying to the same host.
	transport := pkgnet.NewProxyAutoTransport(maxIdleConns /* max-idle */, maxIdleConns /* max-idle-per-host */)

	return otelhttp.NewTransport(
		transport,
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithMeterProvider(mp),
	)
}

func buildProxyHandler(logger *zap.SugaredLogger, env config, transport http.RoundTripper) *httputil.ReverseProxy {
	target := net.JoinHostPort("127.0.0.1", env.UserPort)
	httpProxy := pkghttp.NewHeaderPruningReverseProxy(target, pkghttp.NoHostOverride, activator.RevisionHeaders, false /* use HTTP */)
	httpProxy.Transport = transport
	httpProxy.ErrorHandler = pkghandler.Error(logger)
	httpProxy.BufferPool = netproxy.NewBufferPool()
	httpProxy.FlushInterval = netproxy.FlushInterval

	return httpProxy
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

func requestLogHandler(logger *zap.SugaredLogger, currentHandler http.Handler, env config) http.Handler {
	revInfo := &pkghttp.RequestLogRevision{
		Name:          env.ServingRevision,
		Namespace:     env.ServingNamespace,
		Service:       env.ServingService,
		Configuration: env.ServingConfiguration,
		PodName:       env.ServingPod,
		PodIP:         env.ServingPodIP,
	}

	handler, err := pkghttp.NewRequestLogHandler(
		currentHandler,
		logging.NewSyncFileWriter(os.Stdout),
		env.Observability.RequestLogTemplate,
		pkghttp.RequestLogTemplateInputGetterFromRevision(revInfo),
		env.Observability.EnableProbeRequestLog,
	)
	if err != nil {
		logger.Errorw("Error setting up request logger. Request logs will be unavailable.", zap.Error(err))
		return currentHandler
	}
	return handler
}

func requestAppMetricsHandler(
	logger *zap.SugaredLogger,
	currentHandler http.Handler,
	breaker *queue.Breaker,
	mp metric.MeterProvider,
) http.Handler {
	h, err := queue.NewAppRequestMetricsHandler(mp, currentHandler, breaker)
	if err != nil {
		logger.Errorw("Error setting up app request metrics reporter. Request metrics will be unavailable.", zap.Error(err))
		return currentHandler
	}
	return h
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	os.Stdout.Sync()
	os.Stderr.Sync()
}
