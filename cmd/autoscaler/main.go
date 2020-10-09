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
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"k8s.io/apimachinery/pkg/util/wait"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"

	kubeclient "knative.dev/pkg/client/injection/kube/client"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/leaderelection"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/version"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler/bucket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/autoscaler/scaling"
	"knative.dev/serving/pkg/autoscaler/statforwarder"
	"knative.dev/serving/pkg/autoscaler/statserver"
	smetrics "knative.dev/serving/pkg/metrics"
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

func main() {
	// Set up signals so we handle the first shutdown signal gracefully.
	ctx := signals.NewContext()

	// Report stats on Go memory usage every 30 seconds.
	sharedmain.MemStatsOrDie(ctx)

	cfg := sharedmain.ParseAndGetConfigOrDie()

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
	var err error
	if perr := wait.PollImmediate(time.Second, 60*time.Second, func() (bool, error) {
		if err = version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
			log.Print("Failed to get k8s version ", err)
		}
		return err == nil, nil
	}); perr != nil {
		log.Fatal("Timed out attempting to get k8s version: ", err)
	}

	// Set up our logger.
	loggingConfig, err := sharedmain.GetLoggingConfig(ctx)
	if err != nil {
		log.Fatal("Error loading/parsing logging configuration: ", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, component)
	defer flush(logger)
	ctx = logging.WithLogger(ctx, logger)

	// statsCh is the main communication channel between the stats server and multiscaler.
	statsCh := make(chan asmetrics.StatMessage, statsBufferLen)
	defer close(statsCh)

	profilingHandler := profiling.NewHandler(logger, false)

	cmw := configmap.NewInformedWatcher(kubeclient.Get(ctx), system.Namespace())
	// Watch the logging config map and dynamically update logging levels.
	cmw.Watch(logging.ConfigMapName(), logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map
	cmw.Watch(metrics.ConfigMapName(),
		metrics.ConfigMapWatcher(ctx, component, nil /* SecretFetcher */, logger),
		profilingHandler.UpdateFromConfigMap)

	podLister := podinformer.Get(ctx).Lister()

	collector := asmetrics.NewMetricCollector(
		statsScraperFactoryFunc(podLister), logger)

	// Set up scalers.
	// uniScalerFactory depends endpointsInformer to be set.
	multiScaler := scaling.NewMultiScaler(ctx.Done(),
		uniScalerFactoryFunc(podLister, collector), logger)

	controllers := []*controller.Impl{
		kpa.NewController(ctx, cmw, multiScaler),
		metric.NewController(ctx, cmw, collector),
	}

	// Start watching the configs.
	if err := cmw.Start(ctx.Done()); err != nil {
		logger.Fatalw("Failed to start watching configs", zap.Error(err))
	}

	// Start all of the informers and wait for them to sync.
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		logger.Fatalw("Failed to start informers", zap.Error(err))
	}

	cc := componentConfig(ctx, logger)
	ctx = leaderelection.WithStandardLeaderElectorBuilder(ctx, kubeClient, cc)

	// accept is the func to call when this pod owns the Revision for this StatMessage.
	accept := func(sm asmetrics.StatMessage) {
		collector.Record(sm.Key, time.Now(), sm.Stat)
		multiScaler.Poke(sm.Key, sm.Stat)
	}
	f := statforwarder.New(ctx, logger, kubeClient, cc.Identity, bucket.AutoscalerBucketSet(cc.Buckets), accept)

	// Set up a statserver.
	statsServer := statserver.New(statsServerAddr, statsCh, logger, f.IsBktOwner)

	defer f.Cancel()

	go controller.StartAll(ctx, controllers...)

	go func() {
		for sm := range statsCh {
			f.Process(sm)
		}
	}()

	profilingServer := profiling.NewServer(profilingHandler)

	eg, egCtx := errgroup.WithContext(ctx)
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

func uniScalerFactoryFunc(podLister corev1listers.PodLister,
	metricClient asmetrics.MetricClient) scaling.UniScalerFactory {
	return func(decider *scaling.Decider) (scaling.UniScaler, error) {
		configName := decider.Labels[serving.ConfigurationLabelKey]
		if configName == "" {
			return nil, fmt.Errorf("label %q not found or empty in Decider %s", serving.ConfigurationLabelKey, decider.Name)
		}
		revisionName := decider.Labels[serving.RevisionLabelKey]
		if revisionName == "" {
			return nil, fmt.Errorf("label %q not found or empty in Decider %s", serving.RevisionLabelKey, decider.Name)
		}
		serviceName := decider.Labels[serving.ServiceLabelKey] // This can be empty.

		// Create a stats reporter which tags statistics by PA namespace, configuration name, and PA name.
		ctx := smetrics.RevisionContext(decider.Namespace, serviceName, configName, revisionName)

		podAccessor := resources.NewPodAccessor(podLister, decider.Namespace, revisionName)
		return scaling.New(ctx, decider.Namespace, decider.Name, metricClient,
			podAccessor, &decider.Spec), nil
	}
}

func statsScraperFactoryFunc(podLister corev1listers.PodLister) asmetrics.StatsScraperFactory {
	return func(metric *av1alpha1.Metric, logger *zap.SugaredLogger) (asmetrics.StatsScraper, error) {
		if metric.Spec.ScrapeTarget == "" {
			return nil, nil
		}

		revisionName := metric.Labels[serving.RevisionLabelKey]
		if revisionName == "" {
			return nil, fmt.Errorf("label %q not found or empty in Metric %s", serving.RevisionLabelKey, metric.Name)
		}

		podAccessor := resources.NewPodAccessor(podLister, metric.Namespace, revisionName)
		return asmetrics.NewStatsScraper(metric, revisionName, podAccessor, logger), nil
	}
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	metrics.FlushExporter()
}

func componentConfig(ctx context.Context, logger *zap.SugaredLogger) leaderelection.ComponentConfig {
	selfIP, existing := os.LookupEnv("POD_IP")
	if !existing {
		logger.Fatal("POD_IP environment variable not set.")
	}
	if selfIP == "" {
		logger.Fatal("POD_IP environment variable is empty.")
	}

	// Set up leader election config
	leaderElectionConfig, err := sharedmain.GetLeaderElectionConfig(ctx)
	if err != nil {
		logger.Fatal("Error loading leader election configuration: ", err)
	}

	cc := leaderElectionConfig.GetComponentConfig(component)
	cc.LeaseName = func(i uint32) string {
		return bucket.AutoscalerBucketName(i, cc.Buckets)
	}
	cc.Identity = selfIP

	return cc
}
