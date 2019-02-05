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

	"k8s.io/client-go/tools/clientcmd"

	"github.com/knative/serving/cmd/util"
	"github.com/knative/serving/pkg/autoscaler"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/knative/pkg/logging/logkey"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/signals"
	"github.com/knative/pkg/version"
	"github.com/knative/pkg/websocket"
	"github.com/knative/serving/pkg/activator"
	activatorhandler "github.com/knative/serving/pkg/activator/handler"
	activatorutil "github.com/knative/serving/pkg/activator/util"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/http/h2c"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/metrics"
	"github.com/knative/serving/pkg/system"
	"github.com/knative/serving/pkg/utils"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
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
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")

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
				logger.Error("Stat sink not connected")
				continue
			}
			err := statSink.Send(sm)
			if err != nil {
				logger.Errorw("Error while sending stat", zap.Error(err))
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

	clusterConfig, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatalw("Error getting cluster configuration", zap.Error(err))
	}
	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatalw("Error building new kubernetes client", zap.Error(err))
	}
	servingClient, err := clientset.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatalw("Error building serving clientset", zap.Error(err))
	}

	if err := version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
		logger.Fatalf("Version check failed: %v", err)
	}

	reporter, err := activator.NewStatsReporter()
	if err != nil {
		logger.Fatalw("Failed to create stats reporter", zap.Error(err))
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
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.%s:%s", "autoscaler", system.Namespace(), utils.GetClusterDomainName(), "8080")
	logger.Infof("Connecting to autoscaler at %s", autoscalerEndpoint)
	statSink = websocket.NewDurableSendingConnection(autoscalerEndpoint)
	go statReporter(stopCh)

	podName := util.GetRequiredEnvOrFatal("POD_NAME", logger)

	// Create and run our concurrency reporter
	cr := activatorhandler.NewConcurrencyReporter(podName, reqChan, time.NewTicker(time.Second).C, statChan)
	go cr.Run(stopCh)

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
	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace())
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map and dynamically update metrics exporter.
	configMapWatcher.Watch(metrics.ObservabilityConfigName, metrics.UpdateExporterFromConfigMap(component, logger))
	if err = configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalw("Failed to start configuration manager", zap.Error(err))
	}

	http1Srv := h2c.NewServer(":8080", ah)
	go func() {
		if err := http1Srv.ListenAndServe(); err != nil {
			logger.Errorw("Error running HTTP server", zap.Error(err))
		}
	}()

	h2cSrv := h2c.NewServer(":8081", ah)
	go func() {
		if err := h2cSrv.ListenAndServe(); err != nil {
			logger.Errorw("Error running HTTP server", zap.Error(err))
		}
	}()

	<-stopCh
	a.Shutdown()
	http1Srv.Shutdown(context.TODO())
	h2cSrv.Shutdown(context.TODO())
}
