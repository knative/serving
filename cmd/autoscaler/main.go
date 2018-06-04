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
	"net/http"
	"time"

	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"

	"github.com/knative/serving/cmd/util"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	ela_autoscaler "github.com/knative/serving/pkg/autoscaler"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"

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
	currentScale      int32
	logger            *zap.SugaredLogger

	// Revision-level configuration
	concurrencyModel = flag.String("concurrencyModel", string(v1alpha1.RevisionRequestConcurrencyModelMulti), "")

	// Cluster-level configuration
	autoscaleFlagSet        = k8sflag.NewFlagSet("/etc/config-autoscaler")
	enableScaleToZero       = autoscaleFlagSet.Bool("enable-scale-to-zero", false)
	multiConcurrencyTarget  = autoscaleFlagSet.Float64("multi-concurrency-target", 0.0, k8sflag.Required)
	singleConcurrencyTarget = autoscaleFlagSet.Float64("single-concurrency-target", 0.0, k8sflag.Required)
)

func initEnv() {
	elaNamespace = util.GetRequiredEnvOrFatal("ELA_NAMESPACE", logger)
	elaDeployment = util.GetRequiredEnvOrFatal("ELA_DEPLOYMENT", logger)
	elaConfig = util.GetRequiredEnvOrFatal("ELA_CONFIGURATION", logger)
	elaRevision = util.GetRequiredEnvOrFatal("ELA_REVISION", logger)
	elaAutoscalerPort = util.GetRequiredEnvOrFatal("ELA_AUTOSCALER_PORT", logger)
}

func autoscaler() {
	var targetConcurrency *k8sflag.Float64Flag
	switch *concurrencyModel {
	case string(v1alpha1.RevisionRequestConcurrencyModelSingle):
		targetConcurrency = singleConcurrencyTarget
	case string(v1alpha1.RevisionRequestConcurrencyModelMulti):
		targetConcurrency = multiConcurrencyTarget
	default:
		logger.Fatalf("Unrecognized concurrency model: " + *concurrencyModel)
	}
	config := ela_autoscaler.Config{
		TargetConcurrency:    targetConcurrency,
		MaxScaleUpRate:       autoscaleFlagSet.Float64("max-scale-up-rate", 0.0, k8sflag.Required),
		StableWindow:         autoscaleFlagSet.Duration("stable-window", nil, k8sflag.Required),
		PanicWindow:          autoscaleFlagSet.Duration("panic-window", nil, k8sflag.Required),
		ScaleToZeroThreshold: autoscaleFlagSet.Duration("scale-to-zero-threshold", nil, k8sflag.Required, k8sflag.Dynamic),
	}
	a := ela_autoscaler.NewAutoscaler(config, statsReporter)
	ticker := time.NewTicker(2 * time.Second)
	ctx := logging.WithLogger(context.TODO(), logger)

	for {
		select {
		case <-ticker.C:
			scale, ok := a.Scale(ctx, time.Now())
			if ok {
				// Flag guard scale to zero.
				if !enableScaleToZero.Get() && scale == 0 {
					continue
				}

				scaleChan <- scale
			}
		case s := <-statChan:
			a.Record(ctx, s)
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
					logger.Info("Scaling is not keeping up with autoscaling requests.")
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
	statsReporter.Report(ela_autoscaler.DesiredPodCountM, (int64)(podCount))
	if currentScale == podCount {
		return
	}
	dc := kubeClient.ExtensionsV1beta1().Deployments(elaNamespace)
	deployment, err := dc.Get(elaDeployment, metav1.GetOptions{})
	if err != nil {
		logger.Error("Error getting Deployment %q: %s", elaDeployment, zap.Error(err))
		return
	}
	statsReporter.Report(ela_autoscaler.DesiredPodCountM, (int64)(podCount))
	statsReporter.Report(ela_autoscaler.RequestedPodCountM, (int64)(deployment.Status.Replicas))
	statsReporter.Report(ela_autoscaler.ActualPodCountM, (int64)(deployment.Status.ReadyReplicas))

	if *deployment.Spec.Replicas == podCount {
		currentScale = podCount
		return
	}

	logger.Infof("Scaling from %v to %v", currentScale, podCount)
	if podCount == 0 {
		revisionClient := elaClient.ServingV1alpha1().Revisions(elaNamespace)
		revision, err := revisionClient.Get(elaRevision, metav1.GetOptions{})

		if err != nil {
			logger.Errorf("Error getting Revision %q: %s", elaRevision, zap.Error(err))
		}
		revision.Spec.ServingState = v1alpha1.RevisionServingStateReserve
		revision, err = revisionClient.Update(revision)
		if err != nil {
			logger.Errorf("Error updating Revision %q: %s", elaRevision, zap.Error(err))
		}
		currentScale = 0
		return
	}
	deployment.Spec.Replicas = &podCount
	_, err = dc.Update(deployment)
	if err != nil {
		logger.Errorf("Error updating Deployment %q: %s", elaDeployment, err)
	}
	logger.Info("Successfully scaled.")
	currentScale = podCount
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Failed to upgrade http connection to websocket", zap.Error(err))
		return
	}
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			logger.Error("Dropping non-binary message.")
			continue
		}
		dec := gob.NewDecoder(bytes.NewBuffer(msg))
		var stat ela_autoscaler.Stat
		err = dec.Decode(&stat)
		if err != nil {
			logger.Error("Failed to decode stats", zap.Error(err))
			continue
		}
		statChan <- stat
	}
}

func main() {
	flag.Parse()
	logger = logging.NewLoggerFromDefaultConfigMap("loglevel.autoscaler").Named("ela-autoscaler")
	defer logger.Sync()

	initEnv()
	logger = logger.With(
		zap.String(logkey.Namespace, elaNamespace),
		zap.String(logkey.Configuration, elaConfig),
		zap.String(logkey.Revision, elaRevision))

	logger.Info("Starting autoscaler")
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to get in cluster configuration", zap.Error(err))
	}
	config.Timeout = time.Duration(5 * time.Second)
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Fatal("Failed to create a new clientset", zap.Error(err))
	}
	kubeClient = kc
	ec, err := clientset.NewForConfig(config)
	if err != nil {
		logger.Fatal("Failed to create a new clientset", zap.Error(err))
	}
	elaClient = ec

	exporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "autoscaler"})
	if err != nil {
		logger.Fatal("Failed to create prometheus exporter", zap.Error(err))
	}
	view.RegisterExporter(exporter)
	view.SetReportingPeriod(1 * time.Second)

	reporter, err := ela_autoscaler.NewStatsReporter(elaNamespace, elaConfig, elaRevision)
	if err != nil {
		logger.Fatal("Failed to create stats reporter", zap.Error(err))
	}
	statsReporter = reporter

	go autoscaler()
	go scaleSerializer()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.Handle("/metrics", exporter)
	http.ListenAndServe(":"+elaAutoscalerPort, mux)
}
