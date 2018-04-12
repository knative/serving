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

	"github.com/elafros/elafros/pkg/controller"

	"github.com/golang/glog"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	"github.com/elafros/elafros/pkg/controller/configuration"
	"github.com/elafros/elafros/pkg/controller/revision"
	"github.com/elafros/elafros/pkg/controller/route"
	"github.com/elafros/elafros/pkg/controller/service"
	"github.com/elafros/elafros/pkg/signals"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
)

const (
	threadsPerController = 2
	metricsScrapeAddr    = ":9090"
	metricsScrapePath    = "/metrics"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	flag.Parse()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %v", err)
	}

	elaClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building ela clientset: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	elaInformerFactory := informers.NewSharedInformerFactory(elaClient, time.Second*30)

	controllerConfig, err := controller.NewConfig(kubeClient)
	if err != nil {
		glog.Fatalf("Error loading controller config: %v", err)
	}
	// Add new controllers here.
	ctors := []controller.Constructor{
		configuration.NewController,
		revision.NewController,
		route.NewController,
		service.NewController,
	}

	// Build all of our controllers, with the clients constructed above.
	controllers := make([]controller.Interface, 0, len(ctors))
	for _, ctor := range ctors {
		controllers = append(controllers, ctor(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg, *controllerConfig))
	}

	go kubeInformerFactory.Start(stopCh)
	go elaInformerFactory.Start(stopCh)

	// Start all of the controllers.
	for _, ctrlr := range controllers {
		go func(ctrlr controller.Interface) {
			// We don't expect this to return until stop is called,
			// but if it does, propagate it back.
			if err := ctrlr.Run(threadsPerController, stopCh); err != nil {
				glog.Fatalf("Error running controller: %v", err)
			}
		}(ctrlr)
	}

	// Setup the metrics to flow to Prometheus.
	glog.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "elafros"})
	if err != nil {
		glog.Fatalf("failed to create the Prometheus exporter: %v", err)
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(10 * time.Second)

	// Start the endpoint that Prometheus scraper talks to
	srv := &http.Server{Addr: metricsScrapeAddr}
	http.Handle(metricsScrapePath, promExporter)
	go func() {
		glog.Info("Starting metrics listener at %s", metricsScrapeAddr)
		if err := srv.ListenAndServe(); err != nil {
			glog.Infof("Httpserver: ListenAndServe() finished with error: %v", err)
		}
	}()

	<-stopCh

	// Close the http server gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	glog.Flush()
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
