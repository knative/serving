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
	"time"

	"github.com/knative/pkg/configmap"

	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/reconciler"

	"github.com/knative/serving/pkg/system"

	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	vpainformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	buildinformers "github.com/knative/build/pkg/client/informers/externalversions"
	cachingclientset "github.com/knative/caching/pkg/client/clientset/versioned"
	cachinginformers "github.com/knative/caching/pkg/client/informers/externalversions"
	sharedclientset "github.com/knative/pkg/client/clientset/versioned"
	sharedinformers "github.com/knative/pkg/client/informers/externalversions"
	"github.com/knative/pkg/signals"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/service"
)

const (
	threadsPerController = 2
	logLevelKey          = "controller"
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
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, logLevelKey)
	defer logger.Sync()

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

	sharedClient, err := sharedclientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building shared clientset: %v", err)
	}

	servingClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building serving clientset: %v", err)
	}

	buildClient, err := buildclientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building build clientset: %v", err)
	}

	cachingClient, err := cachingclientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building caching clientset: %v", err)
	}

	vpaClient, err := vpa.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building VPA clientset: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	sharedInformerFactory := sharedinformers.NewSharedInformerFactory(sharedClient, time.Second*30)
	servingInformerFactory := informers.NewSharedInformerFactory(servingClient, time.Second*30)
	buildInformerFactory := buildinformers.NewSharedInformerFactory(buildClient, time.Second*30)
	cachingInformerFactory := cachinginformers.NewSharedInformerFactory(cachingClient, time.Second*30)
	vpaInformerFactory := vpainformers.NewSharedInformerFactory(vpaClient, time.Second*30)

	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace)

	opt := reconciler.Options{
		KubeClientSet:    kubeClient,
		SharedClientSet:  sharedClient,
		ServingClientSet: servingClient,
		CachingClientSet: cachingClient,
		BuildClientSet:   buildClient,
		ConfigMapWatcher: configMapWatcher,
		Logger:           logger,
	}

	serviceInformer := servingInformerFactory.Serving().V1alpha1().Services()
	routeInformer := servingInformerFactory.Serving().V1alpha1().Routes()
	configurationInformer := servingInformerFactory.Serving().V1alpha1().Configurations()
	revisionInformer := servingInformerFactory.Serving().V1alpha1().Revisions()
	kpaInformer := servingInformerFactory.Autoscaling().V1alpha1().PodAutoscalers()
	buildInformer := buildInformerFactory.Build().V1alpha1().Builds()
	deploymentInformer := kubeInformerFactory.Apps().V1().Deployments()
	coreServiceInformer := kubeInformerFactory.Core().V1().Services()
	endpointsInformer := kubeInformerFactory.Core().V1().Endpoints()
	configMapInformer := kubeInformerFactory.Core().V1().ConfigMaps()
	virtualServiceInformer := sharedInformerFactory.Networking().V1alpha3().VirtualServices()
	imageInformer := cachingInformerFactory.Caching().V1alpha1().Images()
	vpaInformer := vpaInformerFactory.Poc().V1alpha1().VerticalPodAutoscalers()

	// Build all of our controllers, with the clients constructed above.
	// Add new controllers to this array.
	controllers := []*controller.Impl{
		configuration.NewController(
			opt,
			configurationInformer,
			revisionInformer,
		),
		revision.NewController(
			opt,
			vpaClient,
			revisionInformer,
			kpaInformer,
			buildInformer,
			imageInformer,
			deploymentInformer,
			coreServiceInformer,
			endpointsInformer,
			configMapInformer,
			vpaInformer,
		),
		route.NewController(
			opt,
			routeInformer,
			configurationInformer,
			revisionInformer,
			coreServiceInformer,
			virtualServiceInformer,
		),
		service.NewController(
			opt,
			serviceInformer,
			configurationInformer,
			routeInformer,
		),
	}

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, logLevelKey))

	// These are non-blocking.
	kubeInformerFactory.Start(stopCh)
	sharedInformerFactory.Start(stopCh)
	servingInformerFactory.Start(stopCh)
	buildInformerFactory.Start(stopCh)
	cachingInformerFactory.Start(stopCh)
	vpaInformerFactory.Start(stopCh)
	if err := configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start configuration manager: %v", err)
	}

	// Wait for the caches to be synced before starting controllers.
	logger.Info("Waiting for informer caches to sync")
	for i, synced := range []cache.InformerSynced{
		serviceInformer.Informer().HasSynced,
		routeInformer.Informer().HasSynced,
		configurationInformer.Informer().HasSynced,
		revisionInformer.Informer().HasSynced,
		kpaInformer.Informer().HasSynced,
		buildInformer.Informer().HasSynced,
		imageInformer.Informer().HasSynced,
		deploymentInformer.Informer().HasSynced,
		coreServiceInformer.Informer().HasSynced,
		endpointsInformer.Informer().HasSynced,
		configMapInformer.Informer().HasSynced,
		virtualServiceInformer.Informer().HasSynced,
	} {
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			logger.Fatalf("failed to wait for cache at index %v to sync", i)
		}
	}

	// Start all of the controllers.
	for _, ctrlr := range controllers {
		go func(ctrlr *controller.Impl) {
			// We don't expect this to return until stop is called,
			// but if it does, propagate it back.
			if runErr := ctrlr.Run(threadsPerController, stopCh); runErr != nil {
				logger.Fatalf("Error running controller: %v", runErr)
			}
		}(ctrlr)
	}

	<-stopCh
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
