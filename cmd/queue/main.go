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
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"knative.dev/serving/pkg/activator"
	activatorutil "knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/autoscaler"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/health"
	queuestats "knative.dev/serving/pkg/queue/stats"
	"github.com/pkg/errors"

	"go.opencensus.io/stats"
	"go.uber.org/zap"

	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/signals"

	"k8s.io/apimachinery/pkg/util/wait"
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

	// Set equal to the queue-proxy's ExecProbe timeout to take
	// advantage of the full window
	probeTimeout = 10 * time.Second

	badProbeTemplate = "unexpected probe header value: %s"

	// Metrics' names (without component prefix).
	requestCountN          = "request_count"
	responseTimeInMsecN    = "request_latencies"
	appRequestCountN       = "app_request_count"
	appResponseTimeInMsecN = "app_request_latencies"

	// requestQueueHealthPath specifies the path for health checks for
	// queue-proxy.
	requestQueueHealthPath = "/health"

	healthURLTemplate = "http://127.0.0.1:%d" + requestQueueHealthPath
)

var (
	servingRevisionKey string
	userTargetAddress  string
	reqChan            = make(chan queue.ReqEvent, requestCountingQueueLength)
	logger             *zap.SugaredLogger
	breaker            *queue.Breaker

	httpProxy *httputil.ReverseProxy

	healthState      = &health.State{}
	promStatReporter *queue.PrometheusStatsReporter // Prometheus stats reporter.

	probe = flag.Bool("probe", false, "run readiness probe")

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
)

type config struct {
	ContainerConcurrency         int    `split_words:"true" required:"true"`
	QueueServingPort             int    `split_words:"true" required:"true"`
	RevisionTimeoutSeconds       int    `split_words:"true" required:"true"`
	UserPort                     int    `split_words:"true" required:"true"`
	EnableVarLogCollection       bool   `split_words:"true"` // optional
	ServingConfiguration         string `split_words:"true" required:"true"`
	ServingNamespace             string `split_words:"true" required:"true"`
	ServingPodIP                 string `split_words:"true" required:"true"`
	ServingPod                   string `split_words:"true" required:"true"`
	ServingRevision              string `split_words:"true" required:"true"`
	ServingService               string `split_words:"true"` // optional
	UserContainerName            string `split_words:"true" required:"true"`
	VarLogVolumeName             string `split_words:"true" required:"true"`
	InternalVolumePath           string `split_words:"true" required:"true"`
	ServingLoggingConfig         string `split_words:"true" required:"true"`
	ServingLoggingLevel          string `split_words:"true" required:"true"`
	ServingRequestMetricsBackend string `split_words:"true" required:"true"`
	ServingRequestLogTemplate    string `split_words:"true" required:"true"`
}

func initConfig(env config) {
	userTargetAddress = "127.0.0.1:" + strconv.Itoa(env.UserPort)
	if env.VarLogVolumeName == "" && env.EnableVarLogCollection {
		logger.Fatal("VAR_LOG_VOLUME_NAME must be specified when ENABLE_VAR_LOG_COLLECTION is true")
	}
	if env.InternalVolumePath == "" && env.EnableVarLogCollection {
		logger.Fatal("INTERNAL_VOLUME_PATH must be specified when ENABLE_VAR_LOG_COLLECTION is true")
	}

	// TODO(mattmoor): Move this key to be in terms of the KPA.
	servingRevisionKey = autoscaler.NewMetricKey(env.ServingNamespace, env.ServingRevision)
	_psr, err := queue.NewPrometheusStatsReporter(env.ServingNamespace, env.ServingConfiguration, env.ServingRevision, env.ServingPod)
	if err != nil {
		logger.Fatalw("Failed to create stats reporter", zap.Error(err))
	}
	promStatReporter = _psr
}

func reportStats(statChan chan *autoscaler.Stat) {
	for s := range statChan {
		if err := promStatReporter.Report(s); err != nil {
			logger.Errorw("Error while sending stat", zap.Error(err))
		}
	}
}

func knativeProbeHeader(r *http.Request) string {
	return r.Header.Get(network.ProbeHeaderName)
}

func knativeProxyHeader(r *http.Request) string {
	return r.Header.Get(network.ProxyHeaderName)
}

func probeUserContainer() bool {
	var err error
	wait.PollImmediate(50*time.Millisecond, probeTimeout, func() (bool, error) {
		logger.Debug("TCP probing the user-container.")
		config := health.TCPProbeConfigOptions{
			Address:       userTargetAddress,
			SocketTimeout: 100 * time.Millisecond,
		}
		err = health.TCPProbe(config)
		return err == nil, nil
	})

	if err == nil {
		logger.Info("User-container successfully probed.")
	} else {
		logger.Errorw("User-container could not be probed successfully.", zap.Error(err))
	}

	return err == nil
}

// Make handler a closure for testing.
func handler(reqChan chan queue.ReqEvent, breaker *queue.Breaker, handler http.Handler) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ph := knativeProbeHeader(r)
		switch {
		case ph != "":
			if ph != queue.Name {
				http.Error(w, fmt.Sprintf(badProbeTemplate, ph), http.StatusBadRequest)
				return
			}
			if probeUserContainer() {
				// Respond with the name of the component handling the request.
				w.Write([]byte(queue.Name))
			} else {
				http.Error(w, "container not ready", http.StatusServiceUnavailable)
			}
			return
		case network.IsKubeletProbe(r):
			// Do not count health checks for concurrency metrics
			handler.ServeHTTP(w, r)
			return
		}

		// Metrics for autoscaling.
		in, out := queue.ReqIn, queue.ReqOut
		if activator.Name == knativeProxyHeader(r) {
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
				handler.ServeHTTP(w, r)
			}) {
				http.Error(w, "overload", http.StatusServiceUnavailable)
			}
		} else {
			handler.ServeHTTP(w, r)
		}
	}
}

// Sets up /health and /wait-for-drain endpoints.
func createAdminHandlers() *http.ServeMux {
	mux := http.NewServeMux()
	// TODO(@joshrider): temporary change while waiting on other PRs to merge (See #4014)
	mux.HandleFunc(requestQueueHealthPath, healthState.HealthHandler(probeUserContainer, true /*isNotAggressive*/))
	mux.HandleFunc(queue.RequestQueueDrainPath, healthState.DrainHandler())

	return mux
}

func probeQueueHealthPath(port int, timeout time.Duration) error {
	url := fmt.Sprintf(healthURLTemplate, port)

	httpClient := &http.Client{
		Transport: &http.Transport{
			// Do not use the cached connection
			DisableKeepAlives: true,
		},
		Timeout: timeout,
	}

	var lastErr error

	// The 25 millisecond retry interval is an unscientific compromise between wanting to get
	// started as early as possible while still wanting to give the container some breathing
	// room to get up and running.
	timeoutErr := wait.PollImmediate(25*time.Millisecond, timeout, func() (bool, error) {
		var res *http.Response
		if res, lastErr = httpClient.Get(url); res == nil {
			return false, nil
		}
		defer res.Body.Close()

		return res.StatusCode == http.StatusOK, nil
	})

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

	if *probe {
		if err := probeQueueHealthPath(networking.QueueAdminPort, probeTimeout); err != nil {
			// used instead of the logger to produce a concise event message
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	var env config
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	logger, _ = logging.NewLogger(env.ServingLoggingConfig, env.ServingLoggingLevel)
	logger = logger.Named("queueproxy")
	defer flush(logger)

	initConfig(env)
	logger = logger.With(
		zap.String(logkey.Key, servingRevisionKey),
		zap.String(logkey.Pod, env.ServingPod))

	target, err := url.Parse("http://" + userTargetAddress)
	if err != nil {
		logger.Fatalw("Failed to parse localhost URL", zap.Error(err))
	}

	httpProxy = httputil.NewSingleHostReverseProxy(target)
	httpProxy.Transport = network.AutoTransport
	httpProxy.FlushInterval = -1

	activatorutil.SetupHeaderPruning(httpProxy)

	// If env.ContainerConcurrency == 0 then concurrency is unlimited.
	if env.ContainerConcurrency > 0 {
		// We set the queue depth to be equal to the container concurrency * 10 to
		// allow the autoscaler to get a strong enough signal.
		queueDepth := env.ContainerConcurrency * 10
		params := queue.BreakerParams{QueueDepth: queueDepth, MaxConcurrency: env.ContainerConcurrency, InitialCapacity: env.ContainerConcurrency}
		breaker = queue.NewBreaker(params)
		logger.Infof("Queue container is starting with %#v", params)
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promStatReporter.Handler())
		http.ListenAndServe(fmt.Sprintf(":%d", networking.AutoscalingQueueMetricsPort), mux)
	}()

	statChan := make(chan *autoscaler.Stat, statReportingQueueLength)
	defer close(statChan)
	go reportStats(statChan)

	reportTicker := time.NewTicker(queue.ReporterReportingPeriod)
	defer reportTicker.Stop()
	queue.NewStats(env.ServingPod, queue.Channels{
		ReqChan:    reqChan,
		ReportChan: reportTicker.C,
		StatChan:   statChan,
	}, time.Now())

	adminServer := &http.Server{
		Addr:    ":" + strconv.Itoa(networking.QueueAdminPort),
		Handler: createAdminHandlers(),
	}

	metricsSupported := false
	if metricsBackend := env.ServingRequestMetricsBackend; metricsBackend != "" {
		if err := setupMetricsExporter(metricsBackend); err == nil {
			metricsSupported = true
			logger.Infof("SERVING_REQUEST_METRICS_BACKEND=%v", metricsBackend)
		} else {
			logger.Errorw("Error setting up request metrics exporter. Request metrics will be unavailable.", zap.Error(err))
		}
	} else {
		logger.Info("SERVING_REQUEST_METRICS_BACKEND is undefined.")
	}

	// Create queue handler chain
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first
	var composedHandler http.Handler = httpProxy
	if metricsSupported {
		composedHandler = pushRequestMetricHandler(httpProxy, appRequestCountM, appResponseTimeInMsecM, env)
	}
	composedHandler = http.HandlerFunc(handler(reqChan, breaker, composedHandler))
	composedHandler = queue.ForwardedShimHandler(composedHandler)
	composedHandler = queue.TimeToFirstByteTimeoutHandler(composedHandler,
		time.Duration(env.RevisionTimeoutSeconds)*time.Second, "request timeout")
	composedHandler = pushRequestLogHandler(composedHandler, env)
	if metricsSupported {
		composedHandler = pushRequestMetricHandler(composedHandler, requestCountM, responseTimeInMsecM, env)
	}
	qSP := strconv.Itoa(env.QueueServingPort)
	logger.Info("Queue-proxy will listen on port ", qSP)
	server := network.NewServer(":"+qSP, composedHandler)

	errChan := make(chan error, 2)
	defer close(errChan)
	// Runs a server created by creator and sends fatal errors to the errChan.
	// Does not act on the ErrServerClosed error since that indicates we're
	// already shutting everything down.
	catchServerError := func(creator func() error) {
		if err := creator(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}

	go catchServerError(server.ListenAndServe)
	go catchServerError(adminServer.ListenAndServe)

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
	case err := <-errChan:
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

func pushRequestMetricHandler(currentHandler http.Handler, countMetric *stats.Int64Measure, latencyMetric *stats.Float64Measure, env config) http.Handler {
	r, err := queuestats.NewStatsReporter(env.ServingNamespace, env.ServingService, env.ServingConfiguration, env.ServingRevision, countMetric, latencyMetric)
	if err != nil {
		logger.Errorw("Error setting up request metrics reporter. Request metrics will be unavailable.", zap.Error(err))
		return currentHandler
	}

	handler, err := queue.NewRequestMetricHandler(currentHandler, r)
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
