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
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/activator"
	activatorutil "knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/autoscaler"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
	"knative.dev/serving/pkg/queue/readiness"
	queuestats "knative.dev/serving/pkg/queue/stats"
)

const (
	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength = 10

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

	// requestQueueHealthPath specifies the path for health checks for
	// queue-proxy.
	requestQueueHealthPath = "/health"

	healthURLTemplate = "http://127.0.0.1:%d" + requestQueueHealthPath
	tcpProbeTimeout   = 100 * time.Millisecond
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
	ContainerConcurrency              int                       `split_words:"true" required:"true"`
	QueueServingPort                  int                       `split_words:"true" required:"true"`
	RevisionTimeoutSeconds            int                       `split_words:"true" required:"true"`
	UserPort                          int                       `split_words:"true" required:"true"`
	EnableVarLogCollection            bool                      `split_words:"true"` // optional
	ServingConfiguration              string                    `split_words:"true" required:"true"`
	ServingNamespace                  string                    `split_words:"true" required:"true"`
	ServingPodIP                      string                    `split_words:"true" required:"true"`
	ServingPod                        string                    `split_words:"true" required:"true"`
	ServingRevision                   string                    `split_words:"true" required:"true"`
	ServingService                    string                    `split_words:"true"` // optional
	UserContainerName                 string                    `split_words:"true" required:"true"`
	VarLogVolumeName                  string                    `split_words:"true" required:"true"`
	InternalVolumePath                string                    `split_words:"true" required:"true"`
	ServingLoggingConfig              string                    `split_words:"true" required:"true"`
	ServingLoggingLevel               string                    `split_words:"true" required:"true"`
	ServingRequestMetricsBackend      string                    `split_words:"true" required:"true"`
	ServingRequestLogTemplate         string                    `split_words:"true" required:"true"`
	ServingReadinessProbe             string                    `split_words:"true" required:"true"`
	TracingConfigDebug                bool                      `split_words:"true"` // optional
	TracingConfigBackend              tracingconfig.BackendType `split_words:"true"` // optional
	TracingConfigSampleRate           float64                   `split_words:"true"` // optional
	TracingConfigZipkinEndpoint       string                    `split_words:"true"` // optional
	TracingConfigStackdriverProjectID string                    `split_words:"true"` // optional
}

// Make handler a closure for testing.
func handler(reqChan chan queue.ReqEvent, breaker *queue.Breaker, handler http.Handler,
	prober func() bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ph := network.KnativeProbeHeader(r)
		switch {
		case ph != "":
			handleKnativeProbe(w, r, ph, prober)
			return
		case network.IsKubeletProbe(r):
			probeCtx, probeSpan := trace.StartSpan(r.Context(), "probe")
			// Do not count health checks for concurrency metrics
			handler.ServeHTTP(w, r.WithContext(probeCtx))
			probeSpan.End()
			return
		}
		proxyCtx, proxySpan := trace.StartSpan(r.Context(), "proxy")
		defer proxySpan.End()
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
			if !breaker.Maybe(r.Context(), func() {
				handler.ServeHTTP(w, r.WithContext(proxyCtx))
			}) {
				http.Error(w, "overload", http.StatusServiceUnavailable)
			}
		} else {
			handler.ServeHTTP(w, r.WithContext(proxyCtx))
		}
	}
}

func handleKnativeProbe(w http.ResponseWriter, r *http.Request, ph string, prober func() bool) {
	_, probeSpan := trace.StartSpan(r.Context(), "probe")
	defer probeSpan.End()

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

	if !prober() {
		http.Error(w, "container not ready", http.StatusServiceUnavailable)
		probeSpan.Annotate([]trace.Attribute{
			trace.StringAttribute("queueproxy.probe.error", "container not ready")}, "error")
		return
	}

	// Respond with the name of the component handling the request.
	w.Write([]byte(queue.Name))
}

func probeQueueHealthPath(port int, timeoutSeconds int) error {
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
		var res *http.Response
		if res, lastErr = httpClient.Get(url); res == nil {
			return false, nil
		}
		defer res.Body.Close()
		return health.IsHTTPProbeReady(res), nil
	}, stopCh)

	if lastErr != nil {
		return errors.Wrap(lastErr, "failed to probe")
	}

	// An http.StatusOK was never returned during probing
	if timeoutErr != nil {
		return errors.New("probe returned not ready")
	}

	return nil
}

func main() {
	flag.Parse()

	// If this is set, we run as a standalone binary to probe the queue-proxy.
	if *readinessProbeTimeout >= 0 {
		if err := probeQueueHealthPath(networking.QueueAdminPort, *readinessProbeTimeout); err != nil {
			// used instead of the logger to produce a concise event message
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Parse the environment.
	var env config
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Setup the logger.
	logger, _ = pkglogging.NewLogger(env.ServingLoggingConfig, env.ServingLoggingLevel)
	logger = logger.Named("queueproxy")
	defer flush(logger)

	logger = logger.With(
		zap.String(logkey.Key, types.NamespacedName{Namespace: env.ServingNamespace, Name: env.ServingRevision}.String()),
		zap.String(logkey.Pod, env.ServingPod))

	if err := validateEnv(env); err != nil {
		logger.Fatal(err.Error())
	}

	// Setup reporters and processes to handle stat reporting.
	promStatReporter, err := queue.NewPrometheusStatsReporter(env.ServingNamespace, env.ServingConfiguration, env.ServingRevision, env.ServingPod, reportingPeriod)
	if err != nil {
		logger.Fatalw("Failed to create stats reporter", zap.Error(err))
	}

	statChan := make(chan *autoscaler.Stat, statReportingQueueLength)
	defer close(statChan)
	go func() {
		for s := range statChan {
			if err := promStatReporter.Report(s); err != nil {
				logger.Errorw("Error while sending stat", zap.Error(err))
			}
		}
	}()

	reqChan := make(chan queue.ReqEvent, requestCountingQueueLength)
	defer close(reqChan)

	reportTicker := time.NewTicker(reportingPeriod)
	defer reportTicker.Stop()

	queue.NewStats(env.ServingPod, queue.Channels{
		ReqChan:    reqChan,
		ReportChan: reportTicker.C,
		StatChan:   statChan,
	}, time.Now())

	// Setup probe to run for checking user-application healthiness.
	probe := buildProbe(env.ServingReadinessProbe)
	healthState := &health.State{}

	server := buildServer(env, probe, reqChan, logger)
	adminServer := buildAdminServer(healthState, probe, logger)
	metricsServer := buildMetricsServer(promStatReporter)

	servers := map[string]*http.Server{
		"main":    server,
		"admin":   adminServer,
		"metrics": metricsServer,
	}

	errCh := make(chan error, len(servers))
	for name, server := range servers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- errors.Wrapf(err, "%s server failed", name)
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
		})

		flush(logger)
		if err := adminServer.Shutdown(context.Background()); err != nil {
			logger.Errorw("Failed to shutdown admin-server", zap.Error(err))
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

func buildServer(env config, rp *readiness.Probe, reqChan chan queue.ReqEvent, logger *zap.SugaredLogger) *http.Server {
	target := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(env.UserPort)),
	}

	httpProxy := httputil.NewSingleHostReverseProxy(target)
	httpProxy.Transport = buildTransport(env, logger)
	httpProxy.ErrorHandler = network.ErrorHandler(logger)

	httpProxy.FlushInterval = -1
	activatorutil.SetupHeaderPruning(httpProxy)

	breaker := buildBreaker(env)
	metricsSupported := supportsMetrics(env, logger)

	// Create queue handler chain.
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first.
	var composedHandler http.Handler = httpProxy
	if metricsSupported {
		composedHandler = pushRequestMetricHandler(httpProxy, appRequestCountM, appResponseTimeInMsecM,
			queueDepthM, breaker, env)
	}
	composedHandler = http.HandlerFunc(handler(reqChan, breaker, composedHandler, rp.ProbeContainer))
	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = queue.TimeToFirstByteTimeoutHandler(composedHandler,
		time.Duration(env.RevisionTimeoutSeconds)*time.Second, "request timeout")
	composedHandler = pushRequestLogHandler(composedHandler, env)

	if metricsSupported {
		composedHandler = pushRequestMetricHandler(composedHandler, requestCountM, responseTimeInMsecM,
			nil /*queueDepthM*/, nil /*breaker*/, env)
	}
	composedHandler = tracing.HTTPSpanMiddleware(composedHandler)
	composedHandler = network.NewProbeHandler(composedHandler)

	return network.NewServer(":"+strconv.Itoa(env.QueueServingPort), composedHandler)
}

func buildTransport(env config, logger *zap.SugaredLogger) http.RoundTripper {
	if env.TracingConfigBackend == tracingconfig.None {
		return network.AutoTransport
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
		Base: network.AutoTransport,
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

func buildAdminServer(healthState *health.State, probe *readiness.Probe, logger *zap.SugaredLogger) *http.Server {
	adminMux := http.NewServeMux()
	adminMux.HandleFunc(requestQueueHealthPath, healthState.HealthHandler(func() bool {
		if !probe.ProbeContainer() {
			return false
		}
		logger.Info("User-container successfully probed.")
		return true
	}, probe.IsAggressive()))
	adminMux.HandleFunc(queue.RequestQueueDrainPath, healthState.DrainHandler())

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
		pkghttp.RequestLogTemplateInputGetterFromRevision(revInfo))

	if err != nil {
		logger.Errorw("Error setting up request logger. Request logs will be unavailable.", zap.Error(err))
		return currentHandler
	}
	return handler
}

func pushRequestMetricHandler(currentHandler http.Handler, countMetric *stats.Int64Measure,
	latencyMetric *stats.Float64Measure, queueDepthMetric *stats.Int64Measure, breaker *queue.Breaker, env config) http.Handler {
	r, err := queuestats.NewStatsReporter(env.ServingNamespace, env.ServingService, env.ServingConfiguration, env.ServingRevision, countMetric, latencyMetric, queueDepthMetric)
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
