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
	"bytes"
	"context"
	"encoding/gob"
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

	"github.com/knative/serving/cmd/util"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	h2cutil "github.com/knative/serving/pkg/h2c"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/system"
	"github.com/knative/serving/third_party/h2c"
	"go.uber.org/zap"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength = 10
	// Add enough buffer to not block request serving on stats collection
	requestCountingQueueLength = 100
	// Number of seconds the /quitquitquit handler should wait before
	// returning.  The purpose is to kill the container alive a little
	// bit longer, that it doesn't go away until the pod is truly
	// removed from service.
	quitSleepSecs = 20

	// Single concurency queue depth.  The maximum number of requests
	// to enqueue before returing 503 overload.
	singleConcurrencyQueueDepth = 10
)

var (
	podName              string
	servingNamespace     string
	servingConfiguration string
	// servingRevision is the revision name prepended with its namespace, e.g.
	// namespace/name.
	servingRevision       string
	servingAutoscaler     string
	servingAutoscalerPort string
	statChan              = make(chan *autoscaler.Stat, statReportingQueueLength)
	reqChan               = make(chan queue.ReqEvent, requestCountingQueueLength)
	kubeClient            *kubernetes.Clientset
	statSink              *websocket.Conn
	logger                *zap.SugaredLogger

	h2cProxy  *httputil.ReverseProxy
	httpProxy *httputil.ReverseProxy

	concurrencyQuantumOfTime = flag.Duration("concurrencyQuantumOfTime", 100*time.Millisecond, "")
	concurrencyModel         = flag.String("concurrencyModel", string(v1alpha1.RevisionRequestConcurrencyModelMulti), "")
	singleConcurrencyBreaker = queue.NewBreaker(singleConcurrencyQueueDepth, 1)
)

func initEnv() {
	podName = util.GetRequiredEnvOrFatal("SERVING_POD", logger)
	servingNamespace = util.GetRequiredEnvOrFatal("SERVING_NAMESPACE", logger)
	servingConfiguration = util.GetRequiredEnvOrFatal("SERVING_CONFIGURATION", logger)
	servingRevision = util.GetRequiredEnvOrFatal("SERVING_REVISION", logger)
	servingAutoscaler = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER", logger)
	servingAutoscalerPort = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER_PORT", logger)
}

func connectStatSink() {
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.cluster.local:%s",
		servingAutoscaler, system.Namespace, servingAutoscalerPort)
	logger.Infof("Connecting to autoscaler at %s.", autoscalerEndpoint)
	for {
		// TODO: use exponential backoff here
		time.Sleep(time.Second)

		dialer := &websocket.Dialer{
			HandshakeTimeout: 3 * time.Second,
		}
		conn, _, err := dialer.Dial(autoscalerEndpoint, nil)
		if err != nil {
			logger.Error("Retrying connection to autoscaler.", zap.Error(err))
		} else {
			logger.Info("Connected to stat sink.")
			statSink = conn
			waitForClose(conn)
		}
	}
}

func waitForClose(c *websocket.Conn) {
	for {
		if _, _, err := c.NextReader(); err != nil {
			logger.Error("Error reading from websocket", zap.Error(err))
			c.Close()
			return
		}
	}
}

func statReporter() {
	for {
		s := <-statChan
		if statSink == nil {
			logger.Error("Stat sink not connected.")
			continue
		}
		sm := autoscaler.StatMessage{
			Stat:        *s,
			RevisionKey: servingRevision,
		}
		var b bytes.Buffer
		enc := gob.NewEncoder(&b)
		err := enc.Encode(sm)
		if err != nil {
			logger.Error("Failed to encode data from stats channel", zap.Error(err))
			continue
		}
		err = statSink.WriteMessage(websocket.BinaryMessage, b.Bytes())
		if err != nil {
			logger.Error("Failed to write to stat sink.", zap.Error(err))
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
	reqChan <- queue.ReqIn
	defer func() {
		reqChan <- queue.ReqOut
	}()
	if *concurrencyModel == string(v1alpha1.RevisionRequestConcurrencyModelSingle) {
		// Enforce single concurrency and breaking
		ok := singleConcurrencyBreaker.Maybe(func() {
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
	h := healthServer{
		alive: true,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueHealthPath), h.healthHandler)
	mux.HandleFunc(fmt.Sprintf("/%s", queue.RequestQueueQuitPath), h.quitHandler)
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
		zap.String(logkey.Namespace, servingNamespace),
		zap.String(logkey.Configuration, servingConfiguration),
		zap.String(logkey.Revision, servingRevision),
		zap.String(logkey.Pod, podName))

	target, err := url.Parse("http://localhost:8080")
	if err != nil {
		logger.Fatal("Failed to parse localhost url", zap.Error(err))
	}

	httpProxy = httputil.NewSingleHostReverseProxy(target)
	h2cProxy = httputil.NewSingleHostReverseProxy(target)
	h2cProxy.Transport = h2cutil.NewTransport()

	logger.Infof("Queue container is starting, concurrencyModel: %s", *concurrencyModel)
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Error getting in cluster config", zap.Error(err))
	}
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Fatal("Error creating new config", zap.Error(err))
	}
	kubeClient = kc
	go connectStatSink()
	go statReporter()
	bucketTicker := time.NewTicker(*concurrencyQuantumOfTime).C
	reportTicker := time.NewTicker(time.Second).C
	queue.NewStats(podName, queue.Channels{
		ReqChan:          reqChan,
		QuantizationChan: bucketTicker,
		ReportChan:       reportTicker,
		StatChan:         statChan,
	})
	defer func() {
		if statSink != nil {
			statSink.Close()
		}
	}()

	adminServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", queue.RequestQueueAdminPort),
		Handler: nil,
	}

	h2cServer := h2c.Server{Server: &http.Server{
		Addr:    fmt.Sprintf(":%d", queue.RequestQueuePort),
		Handler: http.HandlerFunc(handler),
	}}

	// Add a SIGTERM handler to gracefully shutdown the servers during
	// pod termination.
	sigTermChan := make(chan os.Signal)
	signal.Notify(sigTermChan, syscall.SIGTERM)
	go func() {
		<-sigTermChan
		// Calling server.Shutdown() allows pending requests to
		// complete, while no new work is accepted.

		h2cServer.Shutdown(context.Background())
		adminServer.Shutdown(context.Background())
		os.Exit(0)
	}()

	go h2cServer.ListenAndServe()
	setupAdminHandlers(adminServer)
}
