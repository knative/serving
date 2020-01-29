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
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/activator"
	activatorutil "knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/apis/networking"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
	"knative.dev/serving/pkg/queue/readiness"
	queuestats "knative.dev/serving/pkg/queue/stats"
)

const (
	// Add enough buffer to not block request serving on stats collection
	requestCountingQueueLength = 100

	// Duration the /quitquitquit handler should wait before returning.
	// This is to give Istio a little bit more time to remove the pod
	// from its configuration and propagate that to all istio-proxies
	// in the mesh.
	quitSleepDuration = 20 * time.Second

	badProbeTemplate = "unexpected probe header value: %s"

	// Metrics' names (without component prefix).
	requestCountN          = "request_count"
	responseTimeInMsecN    = "request_latencies"
	appRequestCountN       = "app_request_count"
	appResponseTimeInMsecN = "app_request_latencies"
	queueDepthN            = "queue_depth"

	healthURLTemplate = "http://127.0.0.1:%d"
	// The 25 millisecond retry interval is an unscientific compromise between wanting to get
	// started as early as possible while still wanting to give the container some breathing
	// room to get up and running.
	aggressivePollInterval = 25 * time.Millisecond
	// reportingPeriod is the interval of time between reporting stats by queue proxy.
	reportingPeriod = 1 * time.Second
)

var (
	logger *zap.SugaredLogger

	// Metric counters.
	requestCountM = stats.Int64(
		requestCountN,
		"The number of requests that are routed to queue-proxy",
		stats.UnitDimensionless)
	responseTimeInMsecM = stats.Float64(
		responseTimeInMsecN,
		"The response time in millisecond",
		stats.UnitMilliseconds)
	appRequestCountM = stats.Int64(
		appRequestCountN,
		"The number of requests that are routed to user-container",
		stats.UnitDimensionless)
	appResponseTimeInMsecM = stats.Float64(
		appResponseTimeInMsecN,
		"The response time in millisecond",
		stats.UnitMilliseconds)
	queueDepthM = stats.Int64(
		queueDepthN,
		"The current number of items in the serving and waiting queue, or not reported if unlimited concurrency.",
		stats.UnitDimensionless)

	readinessProbeTimeout = flag.Int("probe-period", -1, "run readiness probe with given timeout")
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
	ServingEnableProbeRequestLog bool   `split_words:"true"` // optional

	// Metrics configuration
	ServingNamespace             string `split_words:"true" required:"true"`
	ServingRevision              string `split_words:"true" required:"true"`
	ServingConfiguration         string `split_words:"true" required:"true"`
	ServingPodIP                 string `split_words:"true" required:"true"`
	ServingPod                   string `split_words:"true" required:"true"`
	ServingService               string `split_words:"true"` // optional
	ServingRequestMetricsBackend string `split_words:"true"` // optional

	// /var/log configuration
	EnableVarLogCollection bool   `split_words:"true"` // optional
	UserContainerName      string `split_words:"true"` // optional
	VarLogVolumeName       string `split_words:"true"` // optional
	InternalVolumePath     string `split_words:"true"` // optional

	// Tracing configuration
	TracingConfigDebug                bool                      `split_words:"true"` // optional
	TracingConfigBackend              tracingconfig.BackendType `split_words:"true"` // optional
	TracingConfigSampleRate           float64                   `split_words:"true"` // optional
	TracingConfigZipkinEndpoint       string                    `split_words:"true"` // optional
	TracingConfigStackdriverProjectID string                    `split_words:"true"` // optional
}

// Make handler a closure for testing.
func proxyHandler(reqChan chan queue.ReqEvent, breaker *queue.Breaker, tracingEnabled bool, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if network.IsKubeletProbe(r) {
			next.ServeHTTP(w, r)
			return
		}

		if tracingEnabled {
			proxyCtx, proxySpan := trace.StartSpan(r.Context(), "proxy")
			r = r.WithContext(proxyCtx)
			defer proxySpan.End()
		}

		// Metrics for autoscaling.
		in, out := queue.ReqIn, queue.ReqOut
		if activator.Name == network.KnativeProxyHeader(r) {
			in, out = queue.ProxiedIn, queue.ProxiedOut
		}
		reqChan <- queue.ReqEvent{Time: time.Now(), EventType: in}
		defer func() {
			reqChan <- queue.ReqEvent{Time: time.Now(), EventType: out}
		}()
		network.RewriteHostOut(r)

		// Enforce queuing and concurrency limits.
		if breaker != nil {
			if err := breaker.Maybe(r.Context(), func() {
				next.ServeHTTP(w, r)
			}); err != nil {
				switch err {
				case context.DeadlineExceeded, queue.ErrRequestQueueFull:
					http.Error(w, err.Error(), http.StatusServiceUnavailable)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			}
		} else {
			next.ServeHTTP(w, r)
		}
	}
}

func knativeProbeHandler(healthState *health.State, prober func() bool, isAggressive bool, tracingEnabled bool, next http.Handler) http.HandlerFunc {
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

func probeQueueHealthPath(port int, timeoutSeconds int) error {
	if port <= 0 {
		return fmt.Errorf("port must be a positive value, got %d", port)
	}

	url := fmt.Sprintf(healthURLTemplate, port)
	timeoutDuration := readiness.PollTimeout
	if timeoutSeconds != 0 {
		timeoutDuration = time.Duration(timeoutSeconds) * time.Second
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			// Do not use the cached connection
			DisableKeepAlives: true,
		},
		Timeout: timeoutDuration,
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()
	stopCh := ctx.Done()

	var lastErr error
	// Using PollImmediateUntil instead of PollImmediate because if timeout is reached while waiting for first
	// invocation of conditionFunc, it exits immediately without trying for a second time.
	timeoutErr := wait.PollImmediateUntil(aggressivePollInterval, func() (bool, error) {
		var req *http.Request
		req, lastErr = http.NewRequest(http.MethodGet, url, nil)
		if lastErr != nil {
			// Return nil error for retrying
			return false, nil
		}
		// Add the header to indicate this is a probe request.
		req.Header.Add(network.ProbeHeaderName, queue.Name)
		req.Header.Add(network.UserAgentKey, network.QueueProxyUserAgent)
		res, lastErr := httpClient.Do(req)
		if lastErr != nil {
			// Return nil error for retrying
			return false, nil
		}
		defer res.Body.Close()
		return health.IsHTTPProbeReady(res), nil
	}, stopCh)

	if lastErr != nil {
		return fmt.Errorf("failed to probe: %w", lastErr)
	}

	// An http.StatusOK was never returned during probing
	if timeoutErr != nil {
		return errors.New("probe returned not ready")
	}

	return nil
}

func main() {
	flag.Parse()

	// Parse the environment.
	var env config
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// If this is set, we run as a standalone binary to probe the queue-proxy.
	if *readinessProbeTimeout >= 0 {
		if err := probeQueueHealthPath(env.QueueServingPort, *readinessProbeTimeout); err != nil {
			// used instead of the logger to produce a concise event message
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Setup the logger.
	logger, _ = pkglogging.NewLogger(env.ServingLoggingConfig, env.ServingLoggingLevel)
	logger = logger.Named("queueproxy")
	defer flush(logger)

	logger = logger.With(
		zap.String(logkey.Key, types.NamespacedName{
			Namespace: env.ServingNamespace,
			Name:      env.ServingRevision,
		}.String()),
		zap.String(logkey.Pod, env.ServingPod))

	if err := validateEnv(env); err != nil {
		logger.Fatal(err.Error())
	}

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

	reqChan := make(chan queue.ReqEvent, requestCountingQueueLength)
	defer close(reqChan)

	reportTicker := time.NewTicker(reportingPeriod)
	defer reportTicker.Stop()

	queue.NewStats(time.Now(), reqChan, reportTicker.C, promStatReporter.Report)

	// Setup probe to run for checking user-application healthiness.
	probe := buildProbe(env.ServingReadinessProbe)
	healthState := &health.State{}

	server := buildServer(env, healthState, probe, reqChan, logger)
	adminServer := buildAdminServer(healthState)
	metricsServer := buildMetricsServer(promStatReporter)

	servers := map[string]*http.Server{
		"main":    server,
		"admin":   adminServer,
		"metrics": metricsServer,
	}

	if env.EnableProfiling {
		servers["profile"] = profiling.NewServer(profiling.NewHandler(logger, true))
	}

	errCh := make(chan error, len(servers))
	for name, server := range servers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("%s server failed: %w", name, err)
			}
		}(name, server)
	}

	// Setup /var/log.
	// Logic that isn't required to be executed before the critical path
	// and should be started last to not impact start up latency
	go func() {
		if env.EnableVarLogCollection {
			createVarLogLink(env)
		}
	}()

	// Blocks until we actually receive a TERM signal or one of the servers
	// exit unexpectedly. We fold both signals together because we only want
	// to act on the first of those to reach here.
	select {
	case err := <-errCh:
		logger.Errorw("Failed to bring up queue-proxy, shutting down.", zap.Error(err))
		flush(logger)
		os.Exit(1)
	case <-signals.SetupSignalHandler():
		logger.Info("Received TERM signal, attempting to gracefully shutdown servers.")
		healthState.Shutdown(func() {
			// Give Istio time to sync our "not ready" state.
			time.Sleep(quitSleepDuration)

			// Calling server.Shutdown() allows pending requests to
			// complete, while no new work is accepted.
			if err := server.Shutdown(context.Background()); err != nil {
				logger.Errorw("Failed to shutdown proxy server", zap.Error(err))
			}
			// Removing the main server from the shutdown logic as we've already shut it down.
			delete(servers, "main")
		})

		flush(logger)
		for serverName, srv := range servers {
			if err := srv.Shutdown(context.Background()); err != nil {
				logger.Errorw("Failed to shutdown server", zap.String("server", serverName), zap.Error(err))
			}
		}
	}
}

func validateEnv(env config) error {
	if !env.EnableVarLogCollection {
		return nil
	}

	if env.VarLogVolumeName == "" {
		return errors.New("VAR_LOG_VOLUME_NAME must be specified when ENABLE_VAR_LOG_COLLECTION is true")
	}
	if env.InternalVolumePath == "" {
		return errors.New("INTERNAL_VOLUME_PATH must be specified when ENABLE_VAR_LOG_COLLECTION is true")
	}

	return nil
}

func buildProbe(probeJSON string) *readiness.Probe {
	coreProbe, err := readiness.DecodeProbe(probeJSON)
	if err != nil {
		logger.Fatalw("Queue container failed to parse readiness probe", zap.Error(err))
	}
	return readiness.NewProbe(coreProbe)
}

func buildServer(env config, healthState *health.State, rp *readiness.Probe, reqChan chan queue.ReqEvent,
	logger *zap.SugaredLogger) *http.Server {
	target := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(env.UserPort)),
	}

	httpProxy := httputil.NewSingleHostReverseProxy(target)
	httpProxy.Transport = buildTransport(env, logger)
	httpProxy.ErrorHandler = pkgnet.ErrorHandler(logger)
	httpProxy.BufferPool = network.NewBufferPool()
	httpProxy.FlushInterval = -1
	activatorutil.SetupHeaderPruning(httpProxy)

	breaker := buildBreaker(env)
	metricsSupported := supportsMetrics(env, logger)
	tracingEnabled := env.TracingConfigBackend != tracingconfig.None

	// Create queue handler chain.
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first.
	var composedHandler http.Handler = httpProxy
	if metricsSupported {
		composedHandler = pushRequestMetricHandler(httpProxy, appRequestCountM, appResponseTimeInMsecM,
			queueDepthM, breaker, env)
	}
	composedHandler = proxyHandler(reqChan, breaker, tracingEnabled, composedHandler)
	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = queue.TimeToFirstByteTimeoutHandler(composedHandler,
		time.Duration(env.RevisionTimeoutSeconds)*time.Second, "request timeout")
	composedHandler = pushRequestLogHandler(composedHandler, env)

	if metricsSupported {
		composedHandler = pushRequestMetricHandler(composedHandler, requestCountM, responseTimeInMsecM,
			nil /*queueDepthM*/, nil /*breaker*/, env)
	}
	composedHandler = tracing.HTTPSpanMiddleware(composedHandler)

	composedHandler = knativeProbeHandler(healthState, rp.ProbeContainer, rp.IsAggressive(), tracingEnabled, composedHandler)
	composedHandler = network.NewProbeHandler(composedHandler)

	return pkgnet.NewServer(":"+strconv.Itoa(env.QueueServingPort), composedHandler)
}

func buildTransport(env config, logger *zap.SugaredLogger) http.RoundTripper {
	if env.TracingConfigBackend == tracingconfig.None {
		return pkgnet.AutoTransport
	}

	oct := tracing.NewOpenCensusTracer(tracing.WithExporter(env.ServingPod, logger))
	oct.ApplyConfig(&tracingconfig.Config{
		Backend:              env.TracingConfigBackend,
		Debug:                env.TracingConfigDebug,
		ZipkinEndpoint:       env.TracingConfigZipkinEndpoint,
		StackdriverProjectID: env.TracingConfigStackdriverProjectID,
		SampleRate:           env.TracingConfigSampleRate,
	})

	return &ochttp.Transport{
		Base: pkgnet.AutoTransport,
	}
}

func buildBreaker(env config) *queue.Breaker {
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

func supportsMetrics(env config, logger *zap.SugaredLogger) bool {
	// Setup request metrics reporting for end-user metrics.
	if env.ServingRequestMetricsBackend == "" {
		return false
	}

	if err := setupMetricsExporter(env.ServingRequestMetricsBackend); err != nil {
		logger.Errorw("Error setting up request metrics exporter. Request metrics will be unavailable.", zap.Error(err))
		return false
	}

	return true
}

func buildAdminServer(healthState *health.State) *http.Server {
	adminMux := http.NewServeMux()
	adminMux.HandleFunc(queue.RequestQueueDrainPath, healthState.DrainHandlerFunc())

	return &http.Server{
		Addr:    ":" + strconv.Itoa(networking.QueueAdminPort),
		Handler: adminMux,
	}
}

func buildMetricsServer(promStatReporter *queue.PrometheusStatsReporter) *http.Server {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promStatReporter.Handler())
	return &http.Server{
		Addr:    ":" + strconv.Itoa(networking.AutoscalingQueueMetricsPort),
		Handler: metricsMux,
	}
}

// createVarLogLink creates a symlink allowing the fluentd daemon set to capture the
// logs from the user container /var/log. See fluentd config for more details.
func createVarLogLink(env config) {
	link := strings.Join([]string{env.ServingNamespace, env.ServingPod, env.UserContainerName}, "_")
	target := path.Join("..", env.VarLogVolumeName)
	source := path.Join(env.InternalVolumePath, link)
	if err := os.Symlink(target, source); err != nil {
		logger.Errorw("Failed to create /var/log symlink. Log collection will not work.", zap.Error(err))
	}
}

func pushRequestLogHandler(currentHandler http.Handler, env config) http.Handler {
	if env.ServingRequestLogTemplate == "" {
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

func pushRequestMetricHandler(currentHandler http.Handler, countMetric *stats.Int64Measure,
	latencyMetric *stats.Float64Measure, queueDepthMetric *stats.Int64Measure, breaker *queue.Breaker, env config) http.Handler {
	r, err := queuestats.NewStatsReporter(env.ServingNamespace, env.ServingService, env.ServingConfiguration, env.ServingRevision, env.ServingPod, countMetric, latencyMetric, queueDepthMetric)
	if err != nil {
		logger.Errorw("Error setting up request metrics reporter. Request metrics will be unavailable.", zap.Error(err))
		return currentHandler
	}

	handler, err := queue.NewRequestMetricHandler(currentHandler, r, breaker)
	if err != nil {
		logger.Errorw("Error setting up request metrics handler. Request metrics will be unavailable.", zap.Error(err))
		return currentHandler
	}
	return handler
}

func setupMetricsExporter(backend string) error {
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
		},
	}
	return metrics.UpdateExporter(ops, logger)
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	os.Stdout.Sync()
	os.Stderr.Sync()
	metrics.FlushExporter()
}
