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
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/elafros/elafros/pkg/autoscaler"

	"github.com/golang/glog"
	"github.com/gorilla/websocket"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength   = 10
	requestCountingQueueLength = 10
)

var (
	podName           string
	elaNamespace      string
	elaRevision       string
	elaAutoscaler     string
	elaAutoscalerPort string
	statChan          = make(chan *autoscaler.Stat, statReportingQueueLength)
	reqInChan         = make(chan struct{}, requestCountingQueueLength)
	reqOutChan        = make(chan struct{}, requestCountingQueueLength)
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

	elaNamespace = os.Getenv("ELA_NAMESPACE")
	if elaNamespace == "" {
		glog.Fatal("No ELA_NAMESPACE provided.")
	}
	glog.Infof("ELA_NAMESPACE=%v", elaNamespace)

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
		elaAutoscaler, elaNamespace, elaAutoscalerPort)
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
		glog.Infof("Sending stat: %+v", s)
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

func concurrencyReporter() {
	var concurrentRequests int32 = 0
	var totalRequests int32 = 0
	ticker := time.NewTicker(time.Second).C
	for {
		select {
		case <-ticker:
			now := time.Now()
			stat := &autoscaler.Stat{
				Time:                    &now,
				PodName:                 podName,
				ConcurrentRequests:      concurrentRequests,
				TotalRequestsThisPeriod: totalRequests,
			}
			totalRequests = 0
			statChan <- stat
		case <-reqInChan:
			concurrentRequests = concurrentRequests + 1
			totalRequests = totalRequests + 1
		case <-reqOutChan:
			concurrentRequests = concurrentRequests - 1
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	glog.Info("Request received.")
	var in struct{}
	reqInChan <- in
	defer func() {
		var out struct{}
		reqOutChan <- out
	}()
	glog.Infof("Forwarding a request to the app container at %v", time.Now().String())
	proxy.ServeHTTP(w, r)
}

func main() {
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
	go concurrencyReporter()
	defer func() {
		if statSink != nil {
			statSink.Close()
		}
	}()
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8012", nil)
}
