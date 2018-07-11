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
	"log"
	"net/http"
	"time"

	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"

	"github.com/knative/serving/cmd/util"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/configmap"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"

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
	upgrader              = websocket.Upgrader{}
	servingClient         clientset.Interface
	kubeClient            *kubernetes.Clientset
	statChan              = make(chan autoscaler.Stat, statBufferSize)
	scaleChan             = make(chan int32, scaleBufferSize)
	statsReporter         autoscaler.StatsReporter
	servingNamespace      string
	servingDeployment     string
	servingConfig         string
	servingRevision       string
	servingAutoscalerPort string
	currentScale          int32
	logger                *zap.SugaredLogger

	// Revision-level configuration
	concurrencyModel = flag.String("concurrencyModel", string(v1alpha1.RevisionRequestConcurrencyModelMulti), "")
)

func initEnv() {
	servingNamespace = util.GetRequiredEnvOrFatal("SERVING_NAMESPACE", logger)
	servingDeployment = util.GetRequiredEnvOrFatal("SERVING_DEPLOYMENT", logger)
	servingConfig = util.GetRequiredEnvOrFatal("SERVING_CONFIGURATION", logger)
	servingRevision = util.GetRequiredEnvOrFatal("SERVING_REVISION", logger)
	servingAutoscalerPort = util.GetRequiredEnvOrFatal("SERVING_AUTOSCALER_PORT", logger)
}

func runAutoscaler() {
	switch *concurrencyModel {
	case string(v1alpha1.RevisionRequestConcurrencyModelSingle),
		string(v1alpha1.RevisionRequestConcurrencyModelMulti):
		// It's good.
	default:
		logger.Fatalf("Unrecognized concurrency model: " + *concurrencyModel)
	}
	cm := v1alpha1.RevisionRequestConcurrencyModelType(*concurrencyModel)

	rawConfig, err := configmap.Load("/etc/config-autoscaler")
	if err != nil {
		logger.Fatalf("Error reading config-autoscaler: %v", err)
	}
	config, err := autoscaler.NewConfigFromMap(rawConfig)
	if err != nil {
		logger.Fatalf("Error loading config-autoscaler: %v", err)
	}
	a := autoscaler.New(config, cm, statsReporter)
	ticker := time.NewTicker(2 * time.Second)
	ctx := logging.WithLogger(context.TODO(), logger)

	for {
		select {
		case <-ticker.C:
			scale, ok := a.Scale(ctx, time.Now())
			if ok {
				// Flag guard scale to zero.
				if !config.EnableScaleToZero && scale == 0 {
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
	statsReporter.Report(autoscaler.DesiredPodCountM, (float64)(podCount))
	if currentScale == podCount {
		return
	}
	dc := kubeClient.ExtensionsV1beta1().Deployments(servingNamespace)
	deployment, err := dc.Get(servingDeployment, metav1.GetOptions{})
	if err != nil {
		logger.Error("Error getting Deployment %q: %s", servingDeployment, zap.Error(err))
		return
	}
	statsReporter.Report(autoscaler.DesiredPodCountM, (float64)(podCount))
	statsReporter.Report(autoscaler.RequestedPodCountM, (float64)(deployment.Status.Replicas))
	statsReporter.Report(autoscaler.ActualPodCountM, (float64)(deployment.Status.ReadyReplicas))

	if *deployment.Spec.Replicas == podCount {
		currentScale = podCount
		return
	}

	logger.Infof("Scaling from %v to %v", currentScale, podCount)
	if podCount == 0 {
		revisionClient := servingClient.ServingV1alpha1().Revisions(servingNamespace)
		revision, err := revisionClient.Get(servingRevision, metav1.GetOptions{})

		if err != nil {
			logger.Errorf("Error getting Revision %q: %s", servingRevision, zap.Error(err))
		}
		revision.Spec.ServingState = v1alpha1.RevisionServingStateReserve
		revision, err = revisionClient.Update(revision)
		if err != nil {
			logger.Errorf("Error updating Revision %q: %s", servingRevision, zap.Error(err))
		}
		currentScale = 0
		return
	}
	deployment.Spec.Replicas = &podCount
	_, err = dc.Update(deployment)
	if err != nil {
		logger.Errorf("Error updating Deployment %q: %s", servingDeployment, err)
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
		var sm autoscaler.StatMessage
		err = dec.Decode(&sm)
		if err != nil {
			logger.Error("Failed to decode stats", zap.Error(err))
			continue
		}
		statChan <- sm.Stat
	}
}

func main() {
	flag.Parse()
	loggingConfigMap, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}
	logginConfig, err := logging.NewConfigFromMap(loggingConfigMap)
	if err != nil {
		log.Fatalf("Error parsing logging configuration: %v", err)
	}
	logger, _ = logging.NewLoggerFromConfig(logginConfig, "autoscaler")
	defer logger.Sync()

	initEnv()
	logger = logger.With(
		zap.String(logkey.Namespace, servingNamespace),
		zap.String(logkey.Configuration, servingConfig),
		zap.String(logkey.Revision, servingRevision))

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
	sc, err := clientset.NewForConfig(config)
	if err != nil {
		logger.Fatal("Failed to create a new clientset", zap.Error(err))
	}
	servingClient = sc

	exporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "autoscaler"})
	if err != nil {
		logger.Fatal("Failed to create prometheus exporter", zap.Error(err))
	}
	view.RegisterExporter(exporter)
	view.SetReportingPeriod(1 * time.Second)

	reporter, err := autoscaler.NewStatsReporter(servingNamespace, servingConfig, servingRevision)
	if err != nil {
		logger.Fatal("Failed to create stats reporter", zap.Error(err))
	}
	statsReporter = reporter

	go runAutoscaler()
	go scaleSerializer()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.Handle("/metrics", exporter)
	http.ListenAndServe(":"+servingAutoscalerPort, mux)
}
