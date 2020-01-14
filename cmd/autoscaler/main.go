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
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	basecmd "github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/cmd"
	"github.com/spf13/pflag"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/version"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/pkg/autoscaler/statserver"
	"knative.dev/serving/pkg/reconciler/autoscaling/kpa"
	"knative.dev/serving/pkg/reconciler/metric"
	"knative.dev/serving/pkg/resources"
)

const (
	statsServerAddr = ":8080"
	statsBufferLen  = 1000
	component       = "autoscaler"
	controllerNum   = 2
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

	// Report stats on Go memory usage every 30 seconds.
	msp := metrics.NewMemStatsAll()
	msp.Start(ctx, 30*time.Second)
	if err := view.Register(msp.DefaultViews()...); err != nil {
		log.Fatalf("Error exporting go memstats view: %v", err)
	}

	cfg, err := sharedmain.GetConfig(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatal("Error building kubeconfig:", err)
	}

	log.Printf("Registering %d clients", len(injection.Default.GetClients()))
	log.Printf("Registering %d informer factories", len(injection.Default.GetInformerFactories()))
	log.Printf("Registering %d informers", len(injection.Default.GetInformers()))
	log.Printf("Registering %d controllers", controllerNum)

	// Adjust our client's rate limits based on the number of controller's we are running.
	cfg.QPS = controllerNum * rest.DefaultQPS
	cfg.Burst = controllerNum * rest.DefaultBurst

	ctx, informers := injection.Default.SetupInformers(ctx, cfg)

	kubeClient := kubeclient.Get(ctx)

	// We sometimes startup faster than we can reach kube-api. Poll on failure to prevent us terminating
	if perr := wait.PollImmediate(time.Second, 60*time.Second, func() (bool, error) {
		if err = version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
			log.Printf("Failed to get k8s version %v", err)
		}
		return err == nil, nil
	}); perr != nil {
		log.Fatal("Timed out attempting to get k8s version: ", err)
	}

	// Set up our logger.
	loggingConfig, err := sharedmain.GetLoggingConfig(ctx)
	if err != nil {
		log.Fatal("Error loading/parsing logging configuration:", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, component)
	defer flush(logger)
	ctx = logging.WithLogger(ctx, logger)

	// statsCh is the main communication channel between the stats server and multiscaler.
	statsCh := make(chan autoscaler.StatMessage, statsBufferLen)
	defer close(statsCh)

	profilingHandler := profiling.NewHandler(logger, false)

	cmw := configmap.NewInformedWatcher(kubeclient.Get(ctx), system.Namespace())
	// Watch the logging config map and dynamically update logging levels.
	cmw.Watch(logging.ConfigMapName(), logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map
	cmw.Watch(metrics.ConfigMapName(),
		metrics.UpdateExporterFromConfigMap(component, logger),
		profilingHandler.UpdateFromConfigMap)

	endpointsInformer := endpointsinformer.Get(ctx)

	collector := autoscaler.NewMetricCollector(statsScraperFactoryFunc(endpointsInformer.Lister()), logger)
	customMetricsAdapter.WithCustomMetrics(autoscaler.NewMetricProvider(collector))

	// Set up scalers.
	// uniScalerFactory depends endpointsInformer to be set.
	multiScaler := autoscaler.NewMultiScaler(ctx.Done(), uniScalerFactoryFunc(endpointsInformer, collector), logger)

	controllers := []*controller.Impl{
		kpa.NewController(ctx, cmw, multiScaler),
		metric.NewController(ctx, cmw, collector),
	}

	// Set up a statserver.
	statsServer := statserver.New(statsServerAddr, statsCh, logger)

	// Start watching the configs.
	if err := cmw.Start(ctx.Done()); err != nil {
		logger.Fatalw("Failed to start watching configs", zap.Error(err))
	}

	// Start all of the informers and wait for them to sync.
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		logger.Fatalw("Failed to start informers", zap.Error(err))
	}

	go controller.StartAll(ctx.Done(), controllers...)

	go func() {
		for sm := range statsCh {
			collector.Record(sm.Key, sm.Stat)
			multiScaler.Poke(sm.Key, sm.Stat)
		}
	}()

	profilingServer := profiling.NewServer(profilingHandler)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return customMetricsAdapter.Run(ctx.Done())
	})
	eg.Go(statsServer.ListenAndServe)
	eg.Go(profilingServer.ListenAndServe)

	// This will block until either a signal arrives or one of the grouped functions
	// returns an error.
	<-egCtx.Done()

	statsServer.Shutdown(5 * time.Second)
	profilingServer.Shutdown(context.Background())
	// Don't forward ErrServerClosed as that indicates we're already shutting down.
	if err := eg.Wait(); err != nil && err != http.ErrServerClosed {
		logger.Errorw("Error while running server", zap.Error(err))
	}
}

func uniScalerFactoryFunc(endpointsInformer corev1informers.EndpointsInformer,
	metricClient autoscaler.MetricClient) autoscaler.UniScalerFactory {
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

		return autoscaler.New(decider.Namespace, decider.Name, metricClient, endpointsInformer.Lister(), &decider.Spec, reporter)
	}
}

func statsScraperFactoryFunc(endpointsLister corev1listers.EndpointsLister) autoscaler.StatsScraperFactory {
	return func(metric *av1alpha1.Metric) (autoscaler.StatsScraper, error) {
		podCounter := resources.NewScopedEndpointsCounter(
			endpointsLister, metric.Namespace, metric.Spec.ScrapeTarget)
		return autoscaler.NewServiceScraper(metric, podCounter)
	}
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	metrics.FlushExporter()
}
