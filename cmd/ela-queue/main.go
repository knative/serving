/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/autoscaler"
	"github.com/elafros/elafros/pkg/queue"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength = 10
	// Add enough buffer to keep track of as many requests as can
	// be handled in a quantum of time. Because the request out
	// channel isn't drained until the end of a quantum of time.
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
	podName                  string
	elaRevision              string
	elaAutoscaler            string
	elaAutoscalerPort        string
	statChan                 = make(chan *autoscaler.Stat, statReportingQueueLength)
	reqInChan                = make(chan queue.Poke, requestCountingQueueLength)
	reqOutChan               = make(chan queue.Poke, requestCountingQueueLength)
	kubeClient               *kubernetes.Clientset
	statSink                 *websocket.Conn
	proxy                    *httputil.ReverseProxy
	concurrencyQuantumOfTime = flag.Duration("concurrencyQuantumOfTime", 100*time.Millisecond, "")
	concurrencyModel         = flag.String("concurrencyModel", string(v1alpha1.RevisionRequestConcurrencyModelMulti), "")
	singleConcurrencyBreaker = queue.NewBreaker(singleConcurrencyQueueDepth, 1)
)

func init() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)

	podName = os.Getenv("ELA_POD")
	if podName == "" {
		log.Fatal("No ELA_POD provided.")
	}
	log.Printf("ELA_POD=%v", podName)

	elaRevision = os.Getenv("ELA_REVISION")
	if elaRevision == "" {
		log.Fatal("No ELA_REVISION provided.")
	}
	log.Printf("ELA_REVISION=%v", elaRevision)

	elaAutoscaler = os.Getenv("ELA_AUTOSCALER")
	if elaAutoscaler == "" {
		log.Fatal("No ELA_AUTOSCALER provided.")
	}
	log.Printf("ELA_AUTOSCALER=%v", elaRevision)

	elaAutoscalerPort = os.Getenv("ELA_AUTOSCALER_PORT")
	if elaAutoscalerPort == "" {
		log.Fatal("No ELA_AUTOSCALER_PORT provided.")
	}
	log.Printf("ELA_AUTOSCALER_PORT=%v", elaAutoscalerPort)

	target, err := url.Parse("http://localhost:8080")
	if err != nil {
		log.Fatal(err)
	}
	proxy = httputil.NewSingleHostReverseProxy(target)
}

func connectStatSink() {
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.cluster.local:%s",
		elaAutoscaler, queue.AutoscalerNamespace, elaAutoscalerPort)
	log.Printf("Connecting to autoscaler at %s.", autoscalerEndpoint)
	for {
		// Everything is coming up at the same time.  We wait a
		// second first to let the autoscaler start serving.  And
		// we wait 1 second between attempts to connect so we
		// don't overwhelm the autoscaler.
		time.Sleep(time.Second)

		dialer := &websocket.Dialer{
			HandshakeTimeout: 3 * time.Second,
		}
		conn, _, err := dialer.Dial(autoscalerEndpoint, nil)
		if err != nil {
			log.Print(err)
		} else {
			log.Print("Connected to stat sink.")
			statSink = conn
			return
		}
		log.Print("Retrying connection to autoscaler.")
	}
}

func statReporter() {
	for {
		s := <-statChan
		if statSink == nil {
			log.Print("Stat sink not connected.")
			continue
		}
		var b bytes.Buffer
		enc := gob.NewEncoder(&b)
		err := enc.Encode(s)
		if err != nil {
			log.Print(err)
			continue
		}
		err = statSink.WriteMessage(websocket.BinaryMessage, b.Bytes())
		if err != nil {
			log.Print(err)
			statSink = nil
			log.Print("Attempting reconnection to stat sink.")
			go connectStatSink()
			continue
		}
	}
}

func isProbe(r *http.Request) bool {
	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	return strings.HasPrefix(r.Header.Get("User-Agent"), "kube-probe/")
}

func handler(w http.ResponseWriter, r *http.Request) {
	if isProbe(r) {
		// Do not count health checks for concurrency metrics
		proxy.ServeHTTP(w, r)
		return
	}
	// Metrics for autoscaling
	reqInChan <- queue.Poke{}
	defer func() {
		reqOutChan <- queue.Poke{}
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
	log.Print("Queue container is running")
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error getting in cluster config: %v", err)
	}
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating new config: %v", err)
	}
	kubeClient = kc
	go connectStatSink()
	go statReporter()
	bucketTicker := time.NewTicker(*concurrencyQuantumOfTime).C
	reportTicker := time.NewTicker(time.Second).C
	queue.NewStats(podName, queue.Channels{
		ReqInChan:        reqInChan,
		ReqOutChan:       reqOutChan,
		QuantizationChan: bucketTicker,
		ReportChan:       reportTicker,
		StatChan:         statChan,
	})
	defer func() {
		if statSink != nil {
			statSink.Close()
		}
	}()

	server := &http.Server{
		Addr: fmt.Sprintf(":%d", queue.RequestQueuePort), Handler: nil}
	adminServer := &http.Server{
		Addr: fmt.Sprintf(":%d", queue.RequestQueueAdminPort), Handler: nil}

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
	http.HandleFunc("/", handler)
	go server.ListenAndServe()
	setupAdminHandlers(adminServer)
}
