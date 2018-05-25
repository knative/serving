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
	"errors"
	"flag"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/golang/glog"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller/autoscaling"
	"github.com/knative/serving/pkg/signals"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
)

const (
	threadsPerController = 2
	metricsScrapeAddr    = ":9090"
	metricsScrapePath    = "/metrics"
	elaAutoscalerAddr    = ":8080"
)

var (
	masterURL  string
	kubeconfig string
	eg         errgroup.Group
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

	controller := autoscaling.NewController(kubeClient, elaClient, kubeInformerFactory, elaInformerFactory, cfg)

	go kubeInformerFactory.Start(stopCh)
	go elaInformerFactory.Start(stopCh)

	eg.Go(func() error {
		return controller.Run(threadsPerController, stopCh)
	})

	// Setup the metrics to flow to Prometheus.
	glog.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "elafros"})
	if err != nil {
		glog.Fatalf("failed to create the Prometheus exporter: %v", err)
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(10 * time.Second)

	var wsSrv http.Server
	var metricsSrv http.Server

	eg.Go(func() error {
		mux := http.NewServeMux()
		mux.HandleFunc("/", controller.StatsHandler)
		wsSrv = http.Server{
			Addr:    elaAutoscalerAddr,
			Handler: mux,
		}
		glog.Info("Starting autoscaling HTTP listener at %s", elaAutoscalerAddr)
		return wsSrv.ListenAndServe()
	})

	eg.Go(func() error {
		mux := http.NewServeMux()
		mux.Handle(metricsScrapePath, promExporter)
		metricsSrv = http.Server{
			Addr:    metricsScrapeAddr,
			Handler: mux,
		}
		glog.Info("Starting metrics HTTP listener at %s", metricsScrapeAddr)
		return metricsSrv.ListenAndServe()
	})

	eg.Go(func() error {
		<-stopCh
		return errors.New("Shutting down")
	})

	if err := eg.Wait(); err != nil {
		glog.Error(err)
	}

	// Close the http servers gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsSrv.Shutdown(ctx)
	metricsSrv.Shutdown(ctx)

	glog.Flush()
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
