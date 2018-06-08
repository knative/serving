/*
Copyright 2018 Google LLC

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
	"net/http"
	"time"

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	buildinformers "github.com/knative/build/pkg/client/informers/externalversions"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller/build"
	"github.com/knative/serving/pkg/controller/configuration"
	"github.com/knative/serving/pkg/controller/deployment"
	"github.com/knative/serving/pkg/controller/endpoint"
	"github.com/knative/serving/pkg/controller/ingress"
	"github.com/knative/serving/pkg/controller/revision"
	"github.com/knative/serving/pkg/controller/route"
	"github.com/knative/serving/pkg/controller/service"
	cfgrcv "github.com/knative/serving/pkg/receiver/configuration"
	revrcv "github.com/knative/serving/pkg/receiver/revision"
	rtrcv "github.com/knative/serving/pkg/receiver/route"
	svcrcv "github.com/knative/serving/pkg/receiver/service"
	"github.com/knative/serving/pkg/signals"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
)

const (
	threadsPerController = 2
	metricsScrapeAddr    = ":9090"
	metricsScrapePath    = "/metrics"
)

var (
	masterURL         string
	kubeconfig        string
	queueSidecarImage string
	autoscalerImage   string

	autoscaleFlagSet                  = k8sflag.NewFlagSet("/etc/config-autoscaler")
	autoscaleConcurrencyQuantumOfTime = autoscaleFlagSet.Duration("concurrency-quantum-of-time", nil, k8sflag.Required)
	autoscaleEnableScaleToZero        = autoscaleFlagSet.Bool("enable-scale-to-zero", false)
	autoscaleEnableSingleConcurrency  = autoscaleFlagSet.Bool("enable-single-concurrency", false)

	observabilityFlagSet             = k8sflag.NewFlagSet("/etc/config-observability")
	loggingEnableVarLogCollection    = observabilityFlagSet.Bool("logging.enable-var-log-collection", false)
	loggingFluentSidecarImage        = observabilityFlagSet.String("logging.fluentd-sidecar-image", "")
	loggingFluentSidecarOutputConfig = observabilityFlagSet.String("logging.fluentd-sidecar-output-config", "")
	loggingURLTemplate               = observabilityFlagSet.String("logging.revision-url-template", "")

	loggingFlagSet         = k8sflag.NewFlagSet("/etc/config-logging")
	zapConfig              = loggingFlagSet.String("zap-logger-config", "")
	queueProxyLoggingLevel = loggingFlagSet.String("loglevel.queueproxy", "")
)

func main() {
	flag.Parse()
	logger := logging.NewLoggerFromDefaultConfigMap("loglevel.controller").Named("controller")
	defer logger.Sync()

	if loggingEnableVarLogCollection.Get() {
		if len(loggingFluentSidecarImage.Get()) != 0 {
			logger.Infof("Using fluentd sidecar image: %s", loggingFluentSidecarImage)
		} else {
			logger.Fatal("missing required flag: -fluentdSidecarImage")
		}
		logger.Infof("Using fluentd sidecar output config: %s", loggingFluentSidecarOutputConfig)
	}

	if loggingURLTemplate.Get() != "" {
		logger.Infof("Using logging url template: %s", loggingURLTemplate)
	}

	if len(queueSidecarImage) != 0 {
		logger.Infof("Using queue sidecar image: %s", queueSidecarImage)
	} else {
		logger.Fatal("missing required flag: -queueSidecarImage")
	}

	if len(autoscalerImage) != 0 {
		logger.Infof("Using autoscaler image: %s", autoscalerImage)
	} else {
		logger.Fatal("missing required flag: -autoscalerImage")
	}
	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		logger.Fatalf("Error building kubeconfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building kubernetes clientset: %v", err)
	}

	elaClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building ela clientset: %v", err)
	}

	buildClient, err := buildclientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building build clientset: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	elaInformerFactory := informers.NewSharedInformerFactory(elaClient, time.Second*30)
	buildInformerFactory := buildinformers.NewSharedInformerFactory(buildClient, time.Second*30)

	controllerConfig, err := controller.NewConfig(kubeClient)
	if err != nil {
		logger.Fatalf("Error loading controller config: %v", err)
	}

	revControllerConfig := revrcv.ControllerConfig{
		AutoscaleConcurrencyQuantumOfTime: autoscaleConcurrencyQuantumOfTime,
		AutoscaleEnableSingleConcurrency:  autoscaleEnableSingleConcurrency,
		AutoscalerImage:                   autoscalerImage,
		QueueSidecarImage:                 queueSidecarImage,

		EnableVarLogCollection:     loggingEnableVarLogCollection.Get(),
		FluentdSidecarImage:        loggingFluentSidecarImage.Get(),
		FluentdSidecarOutputConfig: loggingFluentSidecarOutputConfig.Get(),
		LoggingURLTemplate:         loggingURLTemplate.Get(),

		QueueProxyLoggingConfig: zapConfig.Get(),
		QueueProxyLoggingLevel:  queueProxyLoggingLevel.Get(),
	}

	// The receivers are what implement our core logic.  Each of these subscribe to some subset of the resources for which we
	// have controllers below.
	receivers := []interface{}{
		revrcv.New(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, &revControllerConfig, logger),
		rtrcv.New(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, autoscaleEnableScaleToZero, logger),
		cfgrcv.New(kubeClient, elaClient, buildClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger),
		svcrcv.New(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger),
	}

	// Construct a collection of thin workqueue controllers.  Each of these looks for entries in "receivers" that implements foo.Receiver,
	// and on reconciliation events forwards the work to those receivers.
	rtc, err := route.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Route controller: %v", err)
	}
	svc, err := service.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Service controller: %v", err)
	}
	config, err := configuration.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Configuration controller: %v", err)
	}
	rc, err := revision.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Revision controller: %v", err)
	}
	dc, err := deployment.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Deployment controller: %v", err)
	}
	ec, err := endpoint.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Endpoints controller: %v", err)
	}
	bc, err := build.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, buildInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Build controller: %v", err)
	}
	ic, err := ingress.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig, logger, receivers...)
	if err != nil {
		logger.Fatalf("Error creating Ingress controller: %v", err)
	}
	controllers := []controller.Interface{rtc, svc, config, rc, dc, ec, bc, ic}

	go kubeInformerFactory.Start(stopCh)
	go elaInformerFactory.Start(stopCh)
	go buildInformerFactory.Start(stopCh)

	// Start all of the controllers.
	for _, ctrlr := range controllers {
		go func(ctrlr controller.Interface) {
			// We don't expect this to return until stop is called,
			// but if it does, propagate it back.
			if runErr := ctrlr.Run(threadsPerController, stopCh); runErr != nil {
				logger.Fatalf("Error running controller: %v", runErr)
			}
		}(ctrlr)
	}

	// Setup the metrics to flow to Prometheus.
	logger.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "knative"})
	if err != nil {
		logger.Fatalf("failed to create the Prometheus exporter: %v", err)
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(10 * time.Second)

	// Start the endpoint that Prometheus scraper talks to
	srv := &http.Server{Addr: metricsScrapeAddr}
	http.Handle(metricsScrapePath, promExporter)
	go func() {
		logger.Infof("Starting metrics listener at %s", metricsScrapeAddr)
		if err := srv.ListenAndServe(); err != nil {
			logger.Infof("Httpserver: ListenAndServe() finished with error: %v", err)
		}
	}()

	<-stopCh

	// Close the http server gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&queueSidecarImage, "queueSidecarImage", "", "The digest of the queue sidecar image.")
	flag.StringVar(&autoscalerImage, "autoscalerImage", "", "The digest of the autoscaler image.")
}
