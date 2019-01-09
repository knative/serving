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
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/pkg/signals"
	"github.com/knative/pkg/websocket"
	"github.com/knative/serving/cmd/util"
	activatorutil "github.com/knative/serving/pkg/activator/util"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/http/h2c"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/system"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
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
)

var (
	podName                string
	servingConfig          string
	servingNamespace       string
	servingRevision        string
	servingRevisionKey     string
	servingAutoscaler      string
	servingAutoscalerPort  int
	userTargetPort         int
	containerConcurrency   int
	revisionTimeoutSeconds int
	statChan               = make(chan *autoscaler.Stat, statReportingQueueLength)
	reqChan                = make(chan queue.ReqEvent, requestCountingQueueLength)
	statSink               *websocket.ManagedConnection
	logger                 *zap.SugaredLogger
	breaker                *queue.Breaker

	h2cProxy  *httputil.ReverseProxy
	httpProxy *httputil.ReverseProxy

	server   *http.Server
	health   *healthServer
	reporter *queue.Reporter // Prometheus stats reporter.
)

func initEnv() {
	podName = util.GetRequiredEnvOrFatal("SERVING_POD", logger)
	servingConfig = util.GetRequiredEnvOrFatal("SERVING_CONFIGURATION", logger)
	servingNamespace = util.GetRequiredEnvOrFatal("SERVING_NAMESPACE", logger)
	servingRevision = util.GetRequiredEnvOrFatal("SERVING_REVISION", logger)
	servingAutoscaler = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER", logger)
	servingAutoscalerPort = util.MustParseIntEnvOrFatal("SERVING_AUTOSCALER_PORT", logger)
	containerConcurrency = util.MustParseIntEnvOrFatal("CONTAINER_CONCURRENCY", logger)
	revisionTimeoutSeconds = util.MustParseIntEnvOrFatal("REVISION_TIMEOUT_SECONDS", logger)
	userTargetPort = util.MustParseIntEnvOrFatal("USER_PORT", logger)

	// TODO(mattmoor): Move this key to be in terms of the KPA.
	servingRevisionKey = autoscaler.NewMetricKey(servingNamespace, servingRevision)
	health = &healthServer{alive: true}
	_reporter, err := queue.NewStatsReporter(servingNamespace, servingConfig, servingRevision, podName)
	if err != nil {
		logger.Fatal("Failed to create stats reporter", zap.Error(err))
	}
	reporter = _reporter
}

func statReporter() {
	for {
		s := <-statChan
		if err := sendStat(s); err != nil {
			logger.Error("Error while sending stat", zap.Error(err))
		}
	}
}

// sendStat sends a single StatMessage to the autoscaler.
func sendStat(s *autoscaler.Stat) error {
	if statSink == nil {
		return fmt.Errorf("stat sink not (yet) connected")
	}
	if !health.isAlive() {
		s.LameDuck = true
	}
	reporter.Report(
		s.LameDuck,
		float64(s.RequestCount),
		float64(s.AverageConcurrentRequests),
	)
	sm := autoscaler.StatMessage{
		Stat: *s,
		Key:  servingRevisionKey,
	}
	return statSink.Send(sm)
}

func proxyForRequest(req *http.Request) *httputil.ReverseProxy {
	if req.ProtoMajor == 2 {
		return h2cProxy
	}

	return httpProxy
}

func isProbe(r *http.Request) bool {
	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	return strings.HasPrefix(r.Header.Get("User-Agent"), "kube-probe/")
}

func handler(w http.ResponseWriter, r *http.Request) {
	proxy := proxyForRequest(r)

	if isProbe(r) {
		// Do not count health checks for concurrency metrics
		proxy.ServeHTTP(w, r)
		return
	}

	// Metrics for autoscaling
	reqChan <- queue.ReqEvent{Time: time.Now(), EventType: queue.ReqIn}
	defer func() {
		reqChan <- queue.ReqEvent{Time: time.Now(), EventType: queue.ReqOut}
	}()
	// Enforce queuing and concurrency limits
	if breaker != nil {
		ok := breaker.Maybe(func() {
			proxy.ServeHTTP(w, r)
		})
		if !ok {
			http.Error(w, "overload", http.StatusServiceUnavailable)
		}
	} else {
		proxy.ServeHTTP(w, r)
	}
}

// healthServer registers whether a PreStop hook has been called.
type healthServer struct {
	alive bool
	mutex sync.RWMutex
}

// isAlive() returns true until a PreStop hook has been called.
func (h *healthServer) isAlive() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.alive
}

// kill() marks that a PreStop hook has been called.
func (h *healthServer) kill() {
	h.mutex.Lock()
	h.alive = false
	h.mutex.Unlock()
}

// healthHandler is used for readinessProbe/livenessCheck of
// queue-proxy.
func (h *healthServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	if h.isAlive() {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "alive: true")
	} else {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "alive: false")
	}
}

// quitHandler() is used for preStop hook of queue-proxy. It shuts down its main
// server and blocks until it gets successfully shut down.
// This endpoint is also called by the user-container to block its shutdown until
// the queue-proxy's proxy server is shutdown successfully.
func (h *healthServer) quitHandler(w http.ResponseWriter, r *http.Request) {
	// First mark the server as unhealthy to cause lameduck metrics being sent
	h.kill()

	// Force send one (empty) metric to mark the pod as a lameduck before shutting
	// it down.
	now := time.Now()
	s := &autoscaler.Stat{
		Time:     &now,
		PodName:  podName,
		LameDuck: true,
	}
	if err := sendStat(s); err != nil {
		logger.Error("Error while sending stat", zap.Error(err))
	}

	time.Sleep(quitSleepDuration)

	// Shutdown the server.
	currentServer := server
	if currentServer != nil {
		if err := currentServer.Shutdown(context.Background()); err != nil {
			logger.Error("Failed to shutdown proxy-server", zap.Error(err))
		} else {
			logger.Debug("Proxy server shutdown successfully.")
		}
	}

	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "alive: false")
}

// Sets up /health and /quitquitquit endpoints.
func setupAdminHandlers(server *http.Server) {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueHealthPath), health.healthHandler)
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueQuitPath), health.quitHandler)
	server.Handler = mux
}

func main() {
	flag.Parse()
	logger, _ = logging.NewLogger(os.Getenv("SERVING_LOGGING_CONFIG"), os.Getenv("SERVING_LOGGING_LEVEL"))
	logger = logger.Named("queueproxy")
	defer logger.Sync()

	initEnv()
	logger = logger.With(
		zap.String(logkey.Key, servingRevisionKey),
		zap.String(logkey.Pod, podName))

	target, err := url.Parse(fmt.Sprintf("http://localhost:%d", userTargetPort))
	if err != nil {
		logger.Fatal("Failed to parse localhost url", zap.Error(err))
	}

	httpProxy = httputil.NewSingleHostReverseProxy(target)
	h2cProxy = httputil.NewSingleHostReverseProxy(target)
	h2cProxy.Transport = h2c.DefaultTransport

	activatorutil.SetupHeaderPruning(httpProxy)
	activatorutil.SetupHeaderPruning(h2cProxy)

	// If containerConcurrency == 0 then concurrency is unlimited.
	if containerConcurrency > 0 {
		// We set the queue depth to be equal to the container concurrency but at least 10 to
		// allow the autoscaler to get a strong enough signal.
		queueDepth := containerConcurrency
		if queueDepth < 10 {
			queueDepth = 10
		}
		breaker = queue.NewBreaker(int32(queueDepth), int32(containerConcurrency), int32(containerConcurrency))
		logger.Infof("Queue container is starting with queueDepth: %d, containerConcurrency: %d", queueDepth, containerConcurrency)
	}

	logger.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "queue"})
	if err != nil {
		logger.Fatal("Failed to create the Prometheus exporter", zap.Error(err))
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(queue.ReportingPeriod)
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promExporter)
		http.ListenAndServe(fmt.Sprintf(":%d", v1alpha1.RequestQueueMetricsPort), mux)
	}()

	// Open a websocket connection to the autoscaler
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s:%d", servingAutoscaler, system.Namespace, servingAutoscalerPort)
	logger.Infof("Connecting to autoscaler at %s", autoscalerEndpoint)
	statSink = websocket.NewDurableSendingConnection(autoscalerEndpoint)
	go statReporter()

	reportTicker := time.NewTicker(time.Second).C
	queue.NewStats(podName, queue.Channels{
		ReqChan:    reqChan,
		ReportChan: reportTicker,
		StatChan:   statChan,
	}, time.Now())

	adminServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", v1alpha1.RequestQueueAdminPort),
		Handler: nil,
	}
	setupAdminHandlers(adminServer)

	server = h2c.NewServer(
		fmt.Sprintf(":%d", v1alpha1.RequestQueuePort),
		queue.TimeToFirstByteTimeoutHandler(http.HandlerFunc(handler), time.Duration(revisionTimeoutSeconds)*time.Second, "request timeout"))

	// An `ErrServerClosed` should not trigger an early exit of
	// the errgroup below.
	catchServerError := func(runner func() error) func() error {
		return func() error {
			err := runner()
			if err != http.ErrServerClosed {
				return err
			}
			return nil
		}
	}

	var g errgroup.Group
	g.Go(catchServerError(server.ListenAndServe))
	g.Go(catchServerError(adminServer.ListenAndServe))
	g.Go(func() error {
		<-signals.SetupSignalHandler()
		return errors.New("Received SIGTERM")
	})

	if err := g.Wait(); err != nil {
		logger.Error("Shutting down", zap.Error(err))
	}

	// Calling server.Shutdown() allows pending requests to
	// complete, while no new work is accepted.
	if err := adminServer.Shutdown(context.Background()); err != nil {
		logger.Error("Failed to shutdown admin-server", zap.Error(err))
	}

	if statSink != nil {
		if err := statSink.Close(); err != nil {
			logger.Error("Failed to shutdown websocket connection", zap.Error(err))
		}
	}
}
