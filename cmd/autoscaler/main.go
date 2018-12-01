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
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/autoscaler/statserver"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/metrics"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling/hpa"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling/kpa"
	"github.com/knative/serving/pkg/system"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	kubeinformers "k8s.io/client-go/informers"
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
	component         = "autoscaler"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
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
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, component)
	defer logger.Sync()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatal("Error building kubeconfig.", zap.Error(err))
	}

	kubeClientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Fatal("Error building kubernetes clientset.", zap.Error(err))
	}

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher := configmap.NewInformedWatcher(kubeClientSet, system.Namespace)
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map and dynamically update metrics exporter.
	configMapWatcher.Watch(metrics.ObservabilityConfigName, metrics.UpdateExporterFromConfigMap(component, logger))
	// This is based on how Kubernetes sets up its scale client based on discovery:
	// https://github.com/kubernetes/kubernetes/blob/94c2c6c84/cmd/kube-controller-manager/app/autoscaling.go#L75-L81
	restMapper := buildRESTMapper(kubeClientSet, stopCh)
	scaleClient, err := scale.NewForConfig(cfg, restMapper, dynamic.LegacyAPIPathResolverFunc,
		scale.NewDiscoveryScaleKindResolver(kubeClientSet.Discovery()))
	if err != nil {
		logger.Fatal("Error building scale clientset.", zap.Error(err))
	}

	servingClientSet, err := clientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatal("Error building serving clientset.", zap.Error(err))
	}

	rawConfig, err := configmap.Load("/etc/config-autoscaler")
	if err != nil {
		logger.Fatalf("Error reading autoscaler configuration: %v", err)
	}
	dynConfig, err := autoscaler.NewDynamicConfigFromMap(rawConfig, logger)
	if err != nil {
		logger.Fatalf("Error parsing autoscaler configuration: %v", err)
	}
	// Watch the autoscaler config map and dynamically update autoscaler config.
	configMapWatcher.Watch(autoscaler.ConfigName, dynConfig.Update)

	multiScaler := autoscaler.NewMultiScaler(dynConfig, stopCh, uniScalerFactory, logger)

	opt := reconciler.Options{
		KubeClientSet:    kubeClientSet,
		ServingClientSet: servingClientSet,
		Logger:           logger,
	}

	servingInformerFactory := informers.NewSharedInformerFactory(servingClientSet, time.Second*30)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClientSet, time.Second*30)

	paInformer := servingInformerFactory.Autoscaling().V1alpha1().PodAutoscalers()
	endpointsInformer := kubeInformerFactory.Core().V1().Endpoints()
	hpaInformer := kubeInformerFactory.Autoscaling().V1().HorizontalPodAutoscalers()

	kpaScaler := kpa.NewKPAScaler(servingClientSet, scaleClient, logger, configMapWatcher)
	kpaCtl := kpa.NewController(&opt, paInformer, endpointsInformer, multiScaler, kpaScaler, dynConfig)
	hpaCtl := hpa.NewController(&opt, paInformer, hpaInformer)

	// Start the serving informer factory.
	kubeInformerFactory.Start(stopCh)
	servingInformerFactory.Start(stopCh)
	if err := configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start watching logging config: %v", err)
	}

	// Wait for the caches to be synced before starting controllers.
	logger.Info("Waiting for informer caches to sync")
	for i, synced := range []cache.InformerSynced{
		paInformer.Informer().HasSynced,
		endpointsInformer.Informer().HasSynced,
		hpaInformer.Informer().HasSynced,
	} {
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			logger.Fatalf("failed to wait for cache at index %v to sync", i)
		}
	}

	var eg errgroup.Group
	eg.Go(func() error {
		return kpaCtl.Run(controllerThreads, stopCh)
	})
	eg.Go(func() error {
		return hpaCtl.Run(controllerThreads, stopCh)
	})

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
			multiScaler.RecordStat(sm.Key, sm.Stat)
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

func buildRESTMapper(kubeClientSet kubernetes.Interface, stopCh <-chan struct{}) *restmapper.DeferredDiscoveryRESTMapper {
	// This is based on how Kubernetes sets up its discovery-based client:
	// https://github.com/kubernetes/kubernetes/blob/f2c6473e2/cmd/kube-controller-manager/app/controllermanager.go#L410-L414
	cachedClient := cached.NewMemCacheClient(kubeClientSet.Discovery())
	rm := restmapper.NewDeferredDiscoveryRESTMapper(cachedClient)
	go wait.Until(func() {
		rm.Reset()
	}, 30*time.Second, stopCh)

	return rm
}

func uniScalerFactory(metric *autoscaler.Metric, dynamicConfig *autoscaler.DynamicConfig) (autoscaler.UniScaler, error) {
	// Create a stats reporter which tags statistics by PA namespace, configuration name, and PA name.
	reporter, err := autoscaler.NewStatsReporter(metric.Namespace,
		labelValueOrEmpty(metric, serving.ServiceLabelKey), labelValueOrEmpty(metric, serving.ConfigurationLabelKey), metric.Name)
	if err != nil {
		return nil, err
	}

	return autoscaler.New(dynamicConfig, metric.Spec.TargetConcurrency, reporter), nil
}

func labelValueOrEmpty(metric *autoscaler.Metric, labelKey string) string {
	if metric.Labels != nil {
		if value, ok := metric.Labels[labelKey]; ok {
			return value
		}
	}
	return ""
}
