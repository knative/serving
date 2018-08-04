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
	"log"
	"time"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/signals"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/autoscaler/statserver"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling"
	"github.com/knative/serving/pkg/system"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/scale"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	controllerThreads = 2
	statsServerAddr   = ":8080"
	statsBufferLen    = 1000
	logLevelKey       = "autoscaler"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	flag.Parse()

	loggingConfigMap, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}
	loggingConfig, err := logging.NewConfigFromMap(loggingConfigMap)
	if err != nil {
		log.Fatalf("Error parsing logging configuration: %v", err)
	}

	var atomicLevel zap.AtomicLevel
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, logLevelKey)
	defer logger.Sync()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		logger.Fatal("Error building kubeconfig.", zap.Error(err))
	}

	kubeClientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Fatal("Error building kubernetes clientset.", zap.Error(err))
	}

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher := configmap.NewDefaultWatcher(kubeClientSet, system.Namespace)
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, logLevelKey))
	if err := configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start watching logging config: %v", err)
	}

	rm := restmapper.NewDeferredDiscoveryRESTMapper(cached.NewMemCacheClient(kubeClientSet.Discovery()))
	go wait.Until(func() {
		rm.Reset()
	}, 30*time.Second, stopCh)
	scaleKindResolver := scale.NewDiscoveryScaleKindResolver(kubeClientSet.Discovery())
	scaleClient, err := scale.NewForConfig(cfg, rm, dynamic.LegacyAPIPathResolverFunc, scaleKindResolver)
	if err != nil {
		logger.Fatal("Error building kubernetes clientset.", zap.Error(err))
	}

	servingClientSet, err := clientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatal("Error building serving clientset.", zap.Error(err))
	}

	revisionScaler := autoscaler.NewRevisionScaler(servingClientSet, scaleClient, logger)

	rawConfig, err := configmap.Load("/etc/config-autoscaler")
	if err != nil {
		logger.Fatalf("Error reading config-autoscaler: %v", err)
	}
	// TODO: support dynamic modification of the configuration as in, for example, https://github.com/knative/serving/pull/1417.
	config, err := autoscaler.NewConfigFromMap(rawConfig)
	if err != nil {
		logger.Fatalf("Error loading config-autoscaler: %v", err)
	}

	multiScaler := autoscaler.NewMultiScaler(config, revisionScaler, stopCh, uniScalerFactory, logger)

	opt := reconciler.Options{
		KubeClientSet:    kubeClientSet,
		ServingClientSet: servingClientSet,
		Logger:           logger,
	}

	servingInformerFactory := informers.NewSharedInformerFactory(servingClientSet, time.Second*30)
	revisionInformer := servingInformerFactory.Serving().V1alpha1().Revisions()

	ctl := autoscaling.NewController(&opt, revisionInformer, multiScaler, time.Second*30)

	// Start the serving informer factory.
	servingInformerFactory.Start(stopCh)

	// Wait for the caches to be synced before starting controllers.
	logger.Info("Waiting for informer caches to sync")
	for i, synced := range []cache.InformerSynced{
		revisionInformer.Informer().HasSynced,
	} {
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			logger.Fatalf("failed to wait for cache at index %v to sync", i)
		}
	}

	var eg errgroup.Group
	eg.Go(func() error {
		return ctl.Run(controllerThreads, stopCh)
	})

	// Setup the metrics to flow to Prometheus.
	logger.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "autoscaler"})
	if err != nil {
		logger.Fatal("Failed to create the Prometheus exporter.", zap.Error(err))
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(time.Second * 10)

	statsCh := make(chan *autoscaler.StatMessage, statsBufferLen)

	statsServer := statserver.New(statsServerAddr, statsCh, logger)
	eg.Go(func() error {
		return statsServer.ListenAndServe()
	})

	go func() {
		for {
			sm, ok := <-statsCh
			if !ok {
				break
			}
			multiScaler.RecordStat(sm.RevisionKey, sm.Stat)
		}
	}()

	egCh := make(chan struct{})

	go func() {
		if err := eg.Wait(); err != nil {
			logger.Error("Group error.", zap.Error(err))
		}
		close(egCh)
	}()

	select {
	case <-egCh:
	case <-stopCh:
	}

	statsServer.Shutdown(time.Second * 5)
}

func uniScalerFactory(rev *v1alpha1.Revision, config *autoscaler.Config) (autoscaler.UniScaler, error) {
	// Create a stats reporter which tags statistics by revision namespace, revision controller name, and revision name.
	reporter, err := autoscaler.NewStatsReporter(rev.Namespace, revisionControllerName(rev), rev.Name)
	if err != nil {
		return nil, err
	}

	return autoscaler.New(config, rev.Spec.ConcurrencyModel, reporter), nil
}

func revisionControllerName(rev *v1alpha1.Revision) string {
	var controllerName string
	// Get the name of the revision's controller. If the revision has no controller, use the empty string as the
	// controller name.
	if controller := metav1.GetControllerOf(rev); controller != nil {
		controllerName = controller.Name
	}
	return controllerName
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
