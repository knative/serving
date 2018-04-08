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
	"encoding/gob"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/elafros/elafros/pkg/autoscaler"
	"github.com/elafros/elafros/pkg/controller/revision"
	"github.com/elafros/elafros/pkg/queue"

	"github.com/golang/glog"
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
)

var (
	podName           string
	elaRevision       string
	elaAutoscaler     string
	elaAutoscalerPort string
	statChan          = make(chan *autoscaler.Stat, statReportingQueueLength)
	reqInChan         = make(chan queue.Poke, requestCountingQueueLength)
	reqOutChan        = make(chan queue.Poke, requestCountingQueueLength)
	kubeClient        *kubernetes.Clientset
	statSink          *websocket.Conn
	proxy             *httputil.ReverseProxy
)

func init() {
	podName = os.Getenv("ELA_POD")
	if podName == "" {
		glog.Fatal("No ELA_POD provided.")
	}
	glog.Infof("ELA_POD=%v", podName)

	elaRevision = os.Getenv("ELA_REVISION")
	if elaRevision == "" {
		glog.Fatal("No ELA_REVISION provided.")
	}
	glog.Infof("ELA_REVISION=%v", elaRevision)

	elaAutoscaler = os.Getenv("ELA_AUTOSCALER")
	if elaAutoscaler == "" {
		glog.Fatal("No ELA_AUTOSCALER provided.")
	}
	glog.Infof("ELA_AUTOSCALER=%v", elaRevision)

	elaAutoscalerPort = os.Getenv("ELA_AUTOSCALER_PORT")
	if elaAutoscalerPort == "" {
		glog.Fatal("No ELA_AUTOSCALER_PORT provided.")
	}
	glog.Infof("ELA_AUTOSCALER_PORT=%v", elaAutoscalerPort)

	target, err := url.Parse("http://localhost:8080")
	if err != nil {
		glog.Fatal(err)
	}
	proxy = httputil.NewSingleHostReverseProxy(target)
}

func connectStatSink() {
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.cluster.local:%s",
		elaAutoscaler, revision.AutoscalerNamespace, elaAutoscalerPort)
	glog.Infof("Connecting to autoscaler at %s.", autoscalerEndpoint)
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
			glog.Error(err)
		} else {
			glog.Info("Connected to stat sink.")
			statSink = conn
			return
		}
		glog.Error("Retrying connection to autoscaler.")
	}
}

func statReporter() {
	for {
		s := <-statChan
		if statSink == nil {
			glog.Error("Stat sink not connected.")
			continue
		}
		var b bytes.Buffer
		enc := gob.NewEncoder(&b)
		err := enc.Encode(s)
		if err != nil {
			glog.Error(err)
			continue
		}
		err = statSink.WriteMessage(websocket.BinaryMessage, b.Bytes())
		if err != nil {
			glog.Error(err)
			statSink = nil
			glog.Error("Attempting reconnection to stat sink.")
			go connectStatSink()
			continue
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	reqInChan <- queue.Poke{}
	defer func() {
		reqOutChan <- queue.Poke{}
	}()
	proxy.ServeHTTP(w, r)
}

func main() {
	flag.Parse()
	glog.Info("Queue container is running")
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Error getting in cluster config: %v", err)
	}
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Error creating new config: %v", err)
	}
	kubeClient = kc
	go connectStatSink()
	go statReporter()
	// The ratio between reportTicker and bucketTicker durations determines
	// the maximum QPS the autoscaler will target for very short requests.
	// The bucketTicker duration is a quantum of time so only so many can
	// fit into a given duration.  And the autoscaler targets 1.0 average
	// concurrency on average.
	bucketTicker := time.NewTicker(100 * time.Millisecond).C
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
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8012", nil)
}
