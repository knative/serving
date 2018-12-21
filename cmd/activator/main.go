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
	"log"
	"net/http"
	"time"

	"github.com/knative/serving/cmd/util"
	"github.com/knative/serving/pkg/autoscaler"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/knative/pkg/logging/logkey"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/signals"
	"github.com/knative/pkg/websocket"
	"github.com/knative/serving/pkg/activator"
	activatorhandler "github.com/knative/serving/pkg/activator/handler"
	activatorutil "github.com/knative/serving/pkg/activator/util"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/http/h2c"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/metrics"
	"github.com/knative/serving/pkg/system"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	maxUploadBytes = 32e6 // 32MB - same as app engine
	component      = "activator"

	maxRetries             = 18 // the sum of all retries would add up to 1 minute
	minRetryInterval       = 100 * time.Millisecond
	exponentialBackoffBase = 1.3

	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength = 10

	// Add enough buffer to not block request serving on stats collection
	requestCountingQueueLength = 100
)

var (
	logger *zap.SugaredLogger

	statSink *websocket.ManagedConnection
	statChan = make(chan *autoscaler.StatMessage, statReportingQueueLength)
	reqChan  = make(chan activatorhandler.ReqEvent, requestCountingQueueLength)
)

func statReporter(stopCh <-chan struct{}) {
	for {
		select {
		case sm := <-statChan:
			if statSink == nil {
				logger.Error("Stat sink not connected.")
				continue
			}
			err := statSink.Send(sm)
			if err != nil {
				logger.Error("Error while sending stat", zap.Error(err))
			}
		case <-stopCh:
			return
		}
	}
}

func main() {
	flag.Parse()
	cm, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}
	config, err := logging.NewConfigFromMap(cm)
	if err != nil {
		log.Fatalf("Error parsing logging configuration: %v", err)
	}
	createdLogger, atomicLevel := logging.NewLoggerFromConfig(config, component)
	logger = createdLogger.With(zap.String(logkey.ControllerType, "activator"))
	defer logger.Sync()

	logger.Info("Starting the knative activator")

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Error getting in cluster configuration", zap.Error(err))
	}
	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Error building new kubernetes client", zap.Error(err))
	}
	servingClient, err := clientset.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Error building serving clientset", zap.Error(err))
	}

	reporter, err := activator.NewStatsReporter()
	if err != nil {
		logger.Fatal("Failed to create stats reporter", zap.Error(err))
	}

	a := activator.NewRevisionActivator(kubeClient, servingClient, logger)
	a = activator.NewDedupingActivator(a)

	// Retry on 503's for up to 60 seconds. The reason is there is
	// a small delay for k8s to include the ready IP in service.
	// https://github.com/knative/serving/issues/660#issuecomment-384062553
	shouldRetry := activatorutil.RetryStatus(http.StatusServiceUnavailable)
	backoffSettings := wait.Backoff{
		Duration: minRetryInterval,
		Factor:   exponentialBackoffBase,
		Steps:    maxRetries,
	}
	rt := activatorutil.NewRetryRoundTripper(activatorutil.AutoTransport, logger, backoffSettings, shouldRetry)

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	// Open a websocket connection to the autoscaler
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.cluster.local:%s", "autoscaler", system.Namespace, "8080")
	logger.Infof("Connecting to autoscaler at %s", autoscalerEndpoint)
	statSink = websocket.NewDurableSendingConnection(autoscalerEndpoint)
	go statReporter(stopCh)

	podName := util.GetRequiredEnvOrFatal("POD_NAME", logger)
	activatorhandler.NewConcurrencyReporter(podName, activatorhandler.Channels{
		ReqChan:    reqChan,
		StatChan:   statChan,
		ReportChan: time.NewTicker(time.Second).C,
	})

	ah := &activatorhandler.FilteringHandler{
		NextHandler: activatorhandler.NewRequestEventHandler(reqChan,
			&activatorhandler.EnforceMaxContentLengthHandler{
				MaxContentLengthBytes: maxUploadBytes,
				NextHandler: &activatorhandler.ActivationHandler{
					Activator: a,
					Transport: rt,
					Logger:    logger,
					Reporter:  reporter,
				},
			},
		),
	}

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace)
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map and dynamically update metrics exporter.
	configMapWatcher.Watch(metrics.ObservabilityConfigName, metrics.UpdateExporterFromConfigMap(component, logger))
	if err = configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start configuration manager: %v", err)
	}

	srv := h2c.NewServer(":8080", ah)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			logger.Errorf("Error running HTTP server: %v", err)
		}
	}()

	<-stopCh
	a.Shutdown()
	srv.Shutdown(context.TODO())
}
