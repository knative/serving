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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/serving/cmd/util"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/http/h2c"
	kbufferutil "github.com/knative/serving/pkg/kbuffer/util"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/system"
	"github.com/knative/serving/pkg/websocket"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
)

const (
	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength = 10
	// Add enough buffer to not block request serving on stats collection
	requestCountingQueueLength = 100
	// Number of seconds the /quitquitquit handler should wait before
	// returning.  The purpose is to keep the container alive a little
	// bit longer, that it doesn't go away until the pod is truly
	// removed from service.
	quitSleepSecs = 20
)

var (
	podName               string
	servingConfig         string
	servingNamespace      string
	servingRevision       string
	servingRevisionKey    string
	servingAutoscaler     string
	servingAutoscalerPort string
	containerConcurrency  int
	statChan              = make(chan *autoscaler.Stat, statReportingQueueLength)
	reqChan               = make(chan queue.ReqEvent, requestCountingQueueLength)
	statSink              *websocket.ManagedConnection
	logger                *zap.SugaredLogger
	breaker               *queue.Breaker

	h2cProxy  *httputil.ReverseProxy
	httpProxy *httputil.ReverseProxy

	health = queue.NewHealthServer()
)

func initEnv() {
	podName = util.GetRequiredEnvOrFatal("SERVING_POD", logger)
	servingConfig = util.GetRequiredEnvOrFatal("SERVING_CONFIGURATION", logger)
	servingNamespace = util.GetRequiredEnvOrFatal("SERVING_NAMESPACE", logger)
	servingRevision = util.GetRequiredEnvOrFatal("SERVING_REVISION", logger)
	servingAutoscaler = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER", logger)
	servingAutoscalerPort = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER_PORT", logger)
	containerConcurrency = util.MustParseIntEnvOrFatal("CONTAINER_CONCURRENCY", logger)

	// TODO(mattmoor): Move this key to be in terms of the KPA.
	servingRevisionKey = autoscaler.NewKpaKey(servingNamespace, servingRevision)
}

func statReporter(reporter *queue.Reporter) {
	for {
		s := <-statChan
		if statSink == nil {
			logger.Warn("Stat sink not (yet) connected.")
			continue
		}
		lameDuck := 0
		if !health.IsAlive() {
			s.LameDuck = true
			lameDuck = 1
		}
		reporter.Report(
			float64(lameDuck),
			float64(s.RequestCount),
			float64(s.AverageConcurrentRequests),
		)
		sm := autoscaler.StatMessage{
			Stat: *s,
			Key:  servingRevisionKey,
		}
		err := statSink.Send(sm)
		if err != nil {
			logger.Error("Error while sending stat", zap.Error(err))
		}
	}
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

// Sets up /health and /quitquitquit endpoints.
func setupAdminHandlers(server *http.Server) {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueHealthPath), health.HealthHandler)
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueQuitPath), health.QuitHandler)
	server.Handler = mux
	server.ListenAndServe()
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

	target, err := url.Parse("http://localhost:8080")
	if err != nil {
		logger.Fatal("Failed to parse localhost url", zap.Error(err))
	}

	httpProxy = httputil.NewSingleHostReverseProxy(target)
	h2cProxy = httputil.NewSingleHostReverseProxy(target)
	h2cProxy.Transport = h2c.DefaultTransport

	kbufferutil.SetupHeaderPruning(httpProxy)
	kbufferutil.SetupHeaderPruning(h2cProxy)

	// If containerConcurrency == 0 then concurrency is unlimited.
	if containerConcurrency > 0 {
		// We set the queue depth to be equal to the container concurrency but at least 10 to
		// allow the autoscaler to get a strong enough signal.
		queueDepth := containerConcurrency
		if queueDepth < 10 {
			queueDepth = 10
		}
		breaker = queue.NewBreaker(int32(queueDepth), int32(containerConcurrency))
		logger.Infof("Queue container is starting with queueDepth: %d, containerConcurrency: %d", queueDepth, containerConcurrency)
	}

	logger.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "queue"})
	if err != nil {
		logger.Fatal("Failed to create the Prometheus exporter", zap.Error(err))
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(queue.ReportingPeriod)
	reporter, err := queue.NewStatsReporter(servingNamespace, servingConfig, servingRevision)
	if err != nil {
		logger.Fatal("Failed to create stats reporter", zap.Error(err))
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promExporter)
		http.ListenAndServe(":9090", mux)
	}()

	// Open a websocket connection to the autoscaler
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s:%s", servingAutoscaler, system.Namespace, servingAutoscalerPort)
	logger.Infof("Connecting to autoscaler at %s", autoscalerEndpoint)
	statSink = websocket.NewDurableSendingConnection(autoscalerEndpoint)
	go statReporter(reporter)

	reportTicker := time.NewTicker(time.Second).C
	queue.NewStats(podName, queue.Channels{
		ReqChan:    reqChan,
		ReportChan: reportTicker,
		StatChan:   statChan,
	}, time.Now())

	adminServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", queue.RequestQueueAdminPort),
		Handler: nil,
	}

	server := h2c.NewServer(
		fmt.Sprintf(":%d", queue.RequestQueuePort),
		http.HandlerFunc(handler),
	)

	go server.ListenAndServe()
	go setupAdminHandlers(adminServer)

	// Shutdown logic and signal handling
	sigTermChan := make(chan os.Signal)
	signal.Notify(sigTermChan, syscall.SIGTERM)
	// Blocks until we actually receive a TERM signal.
	<-sigTermChan
	// Calling server.Shutdown() allows pending requests to
	// complete, while no new work is accepted.
	logger.Debug("Received shutdown signal, attempting to gracefully shutdown servers.")
	if err := server.Shutdown(context.Background()); err != nil {
		logger.Error("Failed to shutdown proxy-server", zap.Error(err))
	} else {
		logger.Debug("Proxy server shutdown successfully.")
	}
	if err := adminServer.Shutdown(context.Background()); err != nil {
		logger.Error("Failed to shutdown admin-server", zap.Error(err))
	}

	if statSink != nil {
		if err := statSink.Close(); err != nil {
			logger.Error("Failed to shutdown websocket connection", zap.Error(err))
		}
	}
}
