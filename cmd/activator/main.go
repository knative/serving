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
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/knative/pkg/logging/logkey"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/signals"
	"github.com/knative/serving/pkg/activator"
	activatorhandler "github.com/knative/serving/pkg/activator/handler"
	activatorutil "github.com/knative/serving/pkg/activator/util"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"

	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/system"
	"github.com/knative/serving/third_party/h2c"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	maxUploadBytes = 32e6 // 32MB - same as app engine
	logLevelKey    = "activator"

	maxRetries             = 18 // the sum of all retries would add up to 1 minute
	minRetryInterval       = 100 * time.Millisecond
	exponentialBackoffBase = 1.3
)

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
	logger, atomicLevel := logging.NewLoggerFromConfig(config, logLevelKey)
	defer logger.Sync()
	logger = logger.With(zap.String(logkey.ControllerType, "activator"))
	logger.Info("Starting the knative activator")

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Error getting in cluster configuration", zap.Error(err))
	}
	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Error building new config", zap.Error(err))
	}
	servingClient, err := clientset.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Error building serving clientset: %v", zap.Error(err))
	}

	logger.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "activator"})
	if err != nil {
		logger.Fatal("Failed to create the Prometheus exporter: %v", zap.Error(err))
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(10 * time.Second)

	reporter, err := activator.NewStatsReporter()
	if err != nil {
		logger.Fatal("Failed to create stats reporter: %v", zap.Error(err))
	}

	a := activator.NewRevisionActivator(kubeClient, servingClient, logger, reporter)
	a = activator.NewDedupingActivator(a)

	// Retry on 503's for up to 60 seconds. The reason is there is
	// a small delay for k8s to include the ready IP in service.
	// https://github.com/knative/serving/issues/660#issuecomment-384062553
	shouldRetry := activatorutil.RetryStatus(http.StatusServiceUnavailable)
	retryer := activatorutil.NewExponentialRetryer(minRetryInterval, exponentialBackoffBase, maxRetries)

	rt := activatorutil.NewRetryRoundTripper(activatorutil.AutoTransport, logger, retryer, shouldRetry)

	ah := &activatorhandler.ReportingHTTPHandler{
		Reporter: reporter,
		NextHandler: &activatorhandler.EnforceMaxContentLengthHandler{
			MaxContentLengthBytes: maxUploadBytes,
			NextHandler: &activatorhandler.ActivationHandler{
				Activator: a,
				Transport: rt,
				Logger:    logger,
			},
		},
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()
	go func() {
		<-stopCh
		a.Shutdown()
	}()

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher := configmap.NewDefaultWatcher(kubeClient, system.Namespace)
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, logLevelKey))
	if err = configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start configuration manager: %v", err)
	}

	// Start the endpoint for Prometheus scraping
	mux := http.NewServeMux()
	mux.HandleFunc("/", ah.ServeHTTP)
	mux.Handle("/metrics", promExporter)
	h2c.ListenAndServe(":8080", mux)
}
