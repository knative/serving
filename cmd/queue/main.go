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
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/serving/cmd/util"
	activatorutil "github.com/knative/serving/pkg/activator/util"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/http/h2c"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/system"
	"github.com/knative/serving/pkg/websocket"
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
	servingNamespace      string
	servingRevision       string
	servingRevisionKey    string
	servingAutoscaler     string
	servingAutoscalerPort string
	statChan              = make(chan *autoscaler.Stat, statReportingQueueLength)
	reqChan               = make(chan queue.ReqEvent, requestCountingQueueLength)
	statSink              *websocket.ManagedConnection
	logger                *zap.SugaredLogger
	breaker               *queue.Breaker

	h2cProxy  *httputil.ReverseProxy
	httpProxy *httputil.ReverseProxy

	health *healthServer = &healthServer{alive: true}

	containerConcurrency = flag.Int("containerConcurrency", 0, "")
)

func initEnv() {
	podName = util.GetRequiredEnvOrFatal("SERVING_POD", logger)
	servingNamespace = util.GetRequiredEnvOrFatal("SERVING_NAMESPACE", logger)
	servingRevision = util.GetRequiredEnvOrFatal("SERVING_REVISION", logger)
	servingAutoscaler = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER", logger)
	servingAutoscalerPort = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER_PORT", logger)

	// TODO(mattmoor): Move this key to be in terms of the KPA.
	servingRevisionKey = autoscaler.NewKpaKey(servingNamespace, servingRevision)
}

func statReporter() {
	for {
		s := <-statChan
		if statSink == nil {
			logger.Warn("Stat sink not (yet) connected.")
			continue
		}
		if !health.isAlive() {
			s.LameDuck = true
		}
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

// quitHandler() is used for preStop hook of queue-proxy. It:
// - marks the service as not ready, so that requests will no longer
//   be routed to it,
// - adds a small delay, so that the container doesn't get killed at
//   the same time the pod is marked for removal.
func (h *healthServer) quitHandler(w http.ResponseWriter, r *http.Request) {
	// First, we want to mark the container as not ready, so that even
	// if the pod removal (from service) isn't yet effective, the
	// readinessCheck will still prevent traffic to be routed to this
	// pod.
	h.kill()
	// However, since both readinessCheck and pod removal from service
	// is eventually consistent, we add here a small delay to have the
	// container stay alive a little bit longer after.  We still have
	// no guarantee that container termination is done only after
	// removal from service is effective, but this has been showed to
	// alleviate the issue.
	time.Sleep(quitSleepSecs * time.Second)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "alive: false")
}

// Sets up /health and /quitquitquit endpoints.
func setupAdminHandlers(server *http.Server) {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueHealthPath), health.healthHandler)
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueQuitPath), health.quitHandler)
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

	activatorutil.SetupHeaderPruning(httpProxy)
	activatorutil.SetupHeaderPruning(h2cProxy)

	// If containerConcurrency == 0 then concurrency is unlimited.
	if *containerConcurrency > 0 {
		// We set the queue depth to be equal to the container concurrency but at least 10 to
		// allow the autoscaler to get a strong enough signal.
		queueDepth := *containerConcurrency
		if queueDepth < 10 {
			queueDepth = 10
		}
		breaker = queue.NewBreaker(int32(queueDepth), int32(*containerConcurrency))
		logger.Infof("Queue container is starting with queueDepth: %d, containerConcurrency: %d", queueDepth, *containerConcurrency)
	}

	// Open a websocket connection to the autoscaler
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s:%s", servingAutoscaler, system.Namespace, servingAutoscalerPort)
	logger.Infof("Connecting to autoscaler at %s", autoscalerEndpoint)
	statSink = websocket.NewDurableSendingConnection(autoscalerEndpoint)
	go statReporter()

	reportTicker := time.NewTicker(time.Second).C
	queue.NewStats(podName, queue.Channels{
		ReqChan:    reqChan,
		ReportChan: reportTicker,
		StatChan:   statChan,
	}, time.Now())
	defer func() {
		if statSink != nil {
			statSink.Close()
		}
	}()

	adminServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", queue.RequestQueueAdminPort),
		Handler: nil,
	}

	server := h2c.NewServer(
		fmt.Sprintf(":%d", queue.RequestQueuePort),
		http.HandlerFunc(handler),
	)

	// Add a SIGTERM handler to gracefully shutdown the servers during
	// pod termination.
	sigTermChan := make(chan os.Signal)
	signal.Notify(sigTermChan, syscall.SIGTERM)
	go func() {
		<-sigTermChan
		// Calling server.Shutdown() allows pending requests to
		// complete, while no new work is accepted.

		server.Shutdown(context.Background())
		adminServer.Shutdown(context.Background())
		os.Exit(0)
	}()

	go server.ListenAndServe()
	setupAdminHandlers(adminServer)
}
