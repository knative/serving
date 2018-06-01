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
	"log"
	"net/http"
	"os"
	"time"

	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	ela_autoscaler "github.com/knative/serving/pkg/autoscaler"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"

	"github.com/golang/glog"
	"github.com/gorilla/websocket"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// A big enough buffer to handle 1000 pods sending stats every 1
	// second while we do the autoscaling computation (a few hundred
	// milliseconds).
	statBufferSize = 1000

	// Enough buffer to store scale requests generated every 2
	// seconds while an http request is taking the full timeout of 5
	// second.
	scaleBufferSize = 10
)

var (
	upgrader          = websocket.Upgrader{}
	elaClient         clientset.Interface
	kubeClient        *kubernetes.Clientset
	statChan          = make(chan ela_autoscaler.Stat, statBufferSize)
	scaleChan         = make(chan int32, scaleBufferSize)
	statsReporter     ela_autoscaler.StatsReporter
	elaNamespace      string
	elaDeployment     string
	elaConfig         string
	elaRevision       string
	elaAutoscalerPort string

	// Revision-level configuration
	concurrencyModel = flag.String("concurrencyModel", string(v1alpha1.RevisionRequestConcurrencyModelMulti), "")

	// Cluster-level configuration
	enableScaleToZero       = k8sflag.Bool("autoscale.enable-scale-to-zero", false)
	multiConcurrencyTarget  = k8sflag.Float64("autoscale.multi-concurrency-target", 0.0, k8sflag.Required)
	singleConcurrencyTarget = k8sflag.Float64("autoscale.single-concurrency-target", 0.0, k8sflag.Required)
)

func init() {
	elaNamespace = os.Getenv("ELA_NAMESPACE")
	if elaNamespace == "" {
		glog.Fatal("No ELA_NAMESPACE provided.")
	}
	glog.Infof("ELA_NAMESPACE=%v", elaNamespace)

	elaDeployment = os.Getenv("ELA_DEPLOYMENT")
	if elaDeployment == "" {
		glog.Fatal("No ELA_DEPLOYMENT provided.")
	}
	glog.Infof("ELA_DEPLOYMENT=%v", elaDeployment)

	elaConfig = os.Getenv("ELA_CONFIGURATION")
	if elaConfig == "" {
		glog.Fatal("No ELA_CONFIGURATION provided.")
	}
	glog.Infof("ELA_CONFIGURATION=%v", elaConfig)

	elaRevision = os.Getenv("ELA_REVISION")
	if elaRevision == "" {
		glog.Fatal("No ELA_REVISION provided.")
	}
	glog.Infof("ELA_REVISION=%v", elaRevision)

	elaAutoscalerPort = os.Getenv("ELA_AUTOSCALER_PORT")
	if elaAutoscalerPort == "" {
		glog.Fatal("No ELA_AUTOSCALER_PORT provided.")
	}
	glog.Infof("ELA_AUTOSCALER_PORT=%v", elaAutoscalerPort)
}

func autoscaler() {
	var targetConcurrency *k8sflag.Float64Flag
	switch *concurrencyModel {
	case string(v1alpha1.RevisionRequestConcurrencyModelSingle):
		targetConcurrency = singleConcurrencyTarget
	case string(v1alpha1.RevisionRequestConcurrencyModelMulti):
		targetConcurrency = multiConcurrencyTarget
	default:
		log.Fatalf("Unrecognized concurrency model: " + *concurrencyModel)
	}
	config := ela_autoscaler.Config{
		TargetConcurrency:    targetConcurrency,
		MaxScaleUpRate:       k8sflag.Float64("autoscale.max-scale-up-rate", 0.0, k8sflag.Required),
		StableWindow:         k8sflag.Duration("autoscale.stable-window", nil, k8sflag.Required),
		PanicWindow:          k8sflag.Duration("autoscale.panic-window", nil, k8sflag.Required),
		ScaleToZeroThreshold: k8sflag.Duration("autoscale.scale-to-zero-threshold", nil, k8sflag.Required, k8sflag.Dynamic),
	}
	a := ela_autoscaler.NewAutoscaler(config, statsReporter)
	ticker := time.NewTicker(2 * time.Second)

	for {
		select {
		case <-ticker.C:
			scale, ok := a.Scale(time.Now())
			if ok {
				// Flag guard scale to zero.
				if !enableScaleToZero.Get() && scale == 0 {
					continue
				}

				scaleChan <- scale

				// Stop the autoscaler from doing any more work.
				if scale == 0 {
					return
				}
			}
		case s := <-statChan:
			a.Record(s)
		}
	}
}

func scaleSerializer() {
	for {
		select {
		case desiredPodCount := <-scaleChan:
		FastForward:
			// Fast forward to the most recent desired pod
			// count since the http timeout (5 sec) is more
			// than the autoscaling rate (2 sec) and there
			// could be multiple pending scale requests.
			for {
				select {
				case p := <-scaleChan:
					glog.Warning("Scaling is not keeping up with autoscaling requests.")
					desiredPodCount = p
				default:
					break FastForward
				}
			}
			scaleTo(desiredPodCount)
		}
	}
}

func scaleTo(podCount int32) {
	dc := kubeClient.ExtensionsV1beta1().Deployments(elaNamespace)
	deployment, err := dc.Get(elaDeployment, metav1.GetOptions{})
	if err != nil {
		glog.Error("Error getting Deployment %q: %s", elaDeployment, err)
		return
	}
	glog.Infof("===SCALE=== %v %v %v %v %v",
		time.Now().Unix(),
		podCount,
		deployment.Status.Replicas,
		deployment.Status.AvailableReplicas,
		deployment.Status.ReadyReplicas)
	statsReporter.Report(ela_autoscaler.DesiredPodCountM, (int64)(podCount))
	statsReporter.Report(ela_autoscaler.RequestedPodCountM, (int64)(deployment.Status.Replicas))
	statsReporter.Report(ela_autoscaler.ActualPodCountM, (int64)(deployment.Status.ReadyReplicas))

	if *deployment.Spec.Replicas == podCount {
		return
	}

	glog.Infof("Scaling to %v", podCount)
	if podCount == 0 {
		revisionClient := elaClient.KnativeV1alpha1().Revisions(elaNamespace)
		revision, err := revisionClient.Get(elaRevision, metav1.GetOptions{})

		if err != nil {
			glog.Errorf("Error getting Revision %q: %s", elaRevision, err)
		}
		revision.Spec.ServingState = v1alpha1.RevisionServingStateReserve
		revision, err = revisionClient.Update(revision)
		if err != nil {
			glog.Errorf("Error updating Revision %q: %s", elaRevision, err)
		}

	}
	deployment.Spec.Replicas = &podCount
	_, err = dc.Update(deployment)
	if err != nil {
		glog.Errorf("Error updating Deployment %q: %s", elaDeployment, err)
	}
	glog.Info("Successfully scaled.")
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		glog.Error(err)
		return
	}
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			glog.Error("Dropping non-binary message.")
			continue
		}
		dec := gob.NewDecoder(bytes.NewBuffer(msg))
		var stat ela_autoscaler.Stat
		err = dec.Decode(&stat)
		if err != nil {
			glog.Error(err)
			continue
		}
		statChan <- stat
	}
}

func main() {
	flag.Parse()
	glog.Info("Autoscaler up")
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err)
	}
	config.Timeout = time.Duration(5 * time.Second)
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}
	kubeClient = kc
	ec, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}
	elaClient = ec

	exporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "autoscaler"})
	if err != nil {
		glog.Fatal(err)
	}
	view.RegisterExporter(exporter)
	view.SetReportingPeriod(1 * time.Second)

	reporter, err := ela_autoscaler.NewStatsReporter(elaNamespace, elaConfig, elaRevision)
	if err != nil {
		glog.Fatal(err)
	}
	statsReporter = reporter

	go autoscaler()
	go scaleSerializer()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.Handle("/metrics", exporter)
	http.ListenAndServe(":"+elaAutoscalerPort, mux)
}
