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

// Multitenant autoscaler executable.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/injection"
	"github.com/knative/pkg/injection/clients/kubeclient"
	endpointsinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/endpoints"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/metrics"
	pkgmetrics "github.com/knative/pkg/metrics"
	"github.com/knative/pkg/signals"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/autoscaler/statserver"
	"github.com/knative/serving/pkg/reconciler/autoscaling/hpa"
	"github.com/knative/serving/pkg/reconciler/autoscaling/kpa"
	"github.com/knative/serving/pkg/resources"
	basecmd "github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/cmd"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	statsServerAddr = ":8080"
	statsBufferLen  = 1000
	component       = "autoscaler"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func main() {
	// Initialize early to get access to flags and merge them with the autoscaler flags.
	customMetricsAdapter := &basecmd.AdapterBase{}
	customMetricsAdapter.Flags().AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	// Set up signals so we handle the first shutdown signal gracefully.
	ctx := signals.NewContext()

	// Set up our logger.
	loggingConfigMap, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatal("Error loading logging configuration:", err)
	}
	loggingConfig, err := logging.NewConfigFromMap(loggingConfigMap)
	if err != nil {
		log.Fatal("Error parsing logging configuration:", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, component)
	defer flush(logger)
	ctx = logging.WithLogger(ctx, logger)

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatalw("Error building kubeconfig", zap.Error(err))
	}

	logger.Infof("Registering %d clients", len(injection.Default.GetClients()))
	logger.Infof("Registering %d informer factories", len(injection.Default.GetInformerFactories()))
	logger.Infof("Registering %d informers", len(injection.Default.GetInformers()))
	logger.Infof("Registering %d controllers", 2)

	// Adjust our client's rate limits based on the number of controller's we are running.
	cfg.QPS = 2 * rest.DefaultQPS
	cfg.Burst = 2 * rest.DefaultBurst

	ctx, informers := injection.Default.SetupInformers(ctx, cfg)

	// statsCh is the main communication channel between the stats channel and multiscaler.
	statsCh := make(chan *autoscaler.StatMessage, statsBufferLen)
	defer close(statsCh)

	cmw := configmap.NewInformedWatcher(kubeclient.Get(ctx), system.Namespace())
	// Watch the logging config map and dynamically update logging levels.
	cmw.Watch(logging.ConfigMapName(), logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map and dynamically update metrics exporter.
	cmw.Watch(metrics.ConfigMapName(), metrics.UpdateExporterFromConfigMap(component, logger))

	endpointsInformer := endpointsinformer.Get(ctx)

	collector := autoscaler.NewMetricCollector(statsScraperFactoryFunc(endpointsInformer.Lister()), logger)
	customMetricsAdapter.WithCustomMetrics(autoscaler.NewMetricProvider(collector))

	// Set up scalers.
	// uniScalerFactory depends endpointsInformer to be set.
	multiScaler := autoscaler.NewMultiScaler(ctx.Done(), uniScalerFactoryFunc(endpointsInformer, collector), logger)

	psInformerFactory := resources.NewPodScalableInformerFactory(ctx)
	controllers := []*controller.Impl{
		kpa.NewController(ctx, cmw, multiScaler, collector, psInformerFactory),
		hpa.NewController(ctx, cmw, collector, psInformerFactory),
	}

	// Set up a statserver.
	statsServer := statserver.New(statsServerAddr, statsCh, logger)

	// Start watching the configs.
	if err := cmw.Start(ctx.Done()); err != nil {
		logger.Fatalw("Failed to start watching configs", zap.Error(err))
	}

	// Start all of the informers and wait for them to sync.
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		logger.Fatalw("Failed to start informers", err)
	}

	go controller.StartAll(ctx.Done(), controllers...)

	go func() {
		for sm := range statsCh {
			collector.Record(sm.Key, sm.Stat)
			multiScaler.Poke(sm.Key, sm.Stat)
		}
	}()

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return customMetricsAdapter.Run(ctx.Done())
	})
	eg.Go(statsServer.ListenAndServe)

	// This will block until either a signal arrives or one of the grouped functions
	// returns an error.
	<-egCtx.Done()

	statsServer.Shutdown(5 * time.Second)
	if err := eg.Wait(); err != nil {
		logger.Errorw("Error while shutting down", zap.Error(err))
	}
}

func uniScalerFactoryFunc(endpointsInformer corev1informers.EndpointsInformer, metricClient autoscaler.MetricClient) func(decider *autoscaler.Decider) (autoscaler.UniScaler, error) {
	return func(decider *autoscaler.Decider) (autoscaler.UniScaler, error) {
		if v, ok := decider.Labels[serving.ConfigurationLabelKey]; !ok || v == "" {
			return nil, fmt.Errorf("label %q not found or empty in Decider %s", serving.ConfigurationLabelKey, decider.Name)
		}
		if decider.Spec.ServiceName == "" {
			return nil, fmt.Errorf("%s decider has empty ServiceName", decider.Name)
		}

		serviceName := decider.Labels[serving.ServiceLabelKey] // This can be empty.
		configName := decider.Labels[serving.ConfigurationLabelKey]

		// Create a stats reporter which tags statistics by PA namespace, configuration name, and PA name.
		reporter, err := autoscaler.NewStatsReporter(decider.Namespace, serviceName, configName, decider.Name)
		if err != nil {
			return nil, err
		}

		podCounter := resources.NewScopedEndpointsCounter(endpointsInformer.Lister(), decider.Namespace, decider.Name)
		return autoscaler.New(decider.Namespace, decider.Name, metricClient, podCounter, decider.Spec, reporter)
	}
}

func statsScraperFactoryFunc(endpointsLister corev1listers.EndpointsLister) func(metric *autoscaler.Metric) (autoscaler.StatsScraper, error) {
	return func(metric *autoscaler.Metric) (autoscaler.StatsScraper, error) {
		podCounter := resources.NewScopedEndpointsCounter(endpointsLister, metric.Namespace, metric.Spec.ScrapeTarget)
		return autoscaler.NewServiceScraper(metric, podCounter)
	}
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	pkgmetrics.FlushExporter()
}
