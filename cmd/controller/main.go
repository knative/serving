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
	"time"

	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/configmap"

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	buildinformers "github.com/knative/build/pkg/client/informers/externalversions"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller/configuration"
	"github.com/knative/serving/pkg/controller/revision"
	"github.com/knative/serving/pkg/controller/route"
	"github.com/knative/serving/pkg/controller/service"
	"github.com/knative/serving/pkg/signals"
	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	vpainformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
)

const (
	threadsPerController = 2
)

var (
	masterURL         string
	kubeconfig        string
	queueSidecarImage string
	autoscalerImage   string

	autoscaleFlagSet                      = k8sflag.NewFlagSet("/etc/config-autoscaler")
	autoscaleConcurrencyQuantumOfTime     = autoscaleFlagSet.Duration("concurrency-quantum-of-time", nil, k8sflag.Required)
	autoscaleEnableScaleToZero            = autoscaleFlagSet.Bool("enable-scale-to-zero", false)
	autoscaleEnableSingleConcurrency      = autoscaleFlagSet.Bool("enable-single-concurrency", false)
	autoscaleEnableVerticalPodAutoscaling = autoscaleFlagSet.Bool("enable-vertical-pod-autoscaling", false)

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
		logger.Info("Single-tenant autoscaler deployments enabled.")
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
	vpaClient, err := vpa.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building VPA clientset: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	elaInformerFactory := informers.NewSharedInformerFactory(elaClient, time.Second*30)
	buildInformerFactory := buildinformers.NewSharedInformerFactory(buildClient, time.Second*30)
	servingSystemInformerFactory := kubeinformers.NewFilteredSharedInformerFactory(kubeClient,
		time.Minute*5, pkg.GetServingSystemNamespace(), nil)
	vpaInformerFactory := vpainformers.NewSharedInformerFactory(vpaClient, time.Second*30)

	revControllerConfig := revision.ControllerConfig{
		AutoscaleConcurrencyQuantumOfTime:     autoscaleConcurrencyQuantumOfTime,
		AutoscaleEnableSingleConcurrency:      autoscaleEnableSingleConcurrency,
		AutoscaleEnableVerticalPodAutoscaling: autoscaleEnableVerticalPodAutoscaling,
		AutoscalerImage:                       autoscalerImage,
		QueueSidecarImage:                     queueSidecarImage,

		EnableVarLogCollection:     loggingEnableVarLogCollection.Get(),
		FluentdSidecarImage:        loggingFluentSidecarImage.Get(),
		FluentdSidecarOutputConfig: loggingFluentSidecarOutputConfig.Get(),
		LoggingURLTemplate:         loggingURLTemplate.Get(),

		QueueProxyLoggingConfig: zapConfig.Get(),
		QueueProxyLoggingLevel:  queueProxyLoggingLevel.Get(),
	}

	configMapWatcher := configmap.NewDefaultWatcher(kubeClient, pkg.GetServingSystemNamespace())

	opt := controller.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: elaClient,
		BuildClientSet:   buildClient,
		ConfigMapWatcher: configMapWatcher,
		Logger:           logger,
	}

	serviceInformer := elaInformerFactory.Serving().V1alpha1().Services()
	routeInformer := elaInformerFactory.Serving().V1alpha1().Routes()
	configurationInformer := elaInformerFactory.Serving().V1alpha1().Configurations()
	revisionInformer := elaInformerFactory.Serving().V1alpha1().Revisions()
	buildInformer := buildInformerFactory.Build().V1alpha1().Builds()
	deploymentInformer := kubeInformerFactory.Apps().V1().Deployments()
	endpointsInformer := kubeInformerFactory.Core().V1().Endpoints()
	coreServiceInformer := kubeInformerFactory.Core().V1().Services()
	ingressInformer := kubeInformerFactory.Extensions().V1beta1().Ingresses()
	vpaInformer := vpaInformerFactory.Poc().V1alpha1().VerticalPodAutoscalers()

	// Build all of our controllers, with the clients constructed above.
	// Add new controllers to this array.
	controllers := []controller.Interface{
		configuration.NewController(opt, configurationInformer, revisionInformer, cfg),
		revision.NewController(opt, vpaClient, revisionInformer, buildInformer,
			deploymentInformer, coreServiceInformer, endpointsInformer, vpaInformer,
			cfg, &revControllerConfig),
		route.NewController(opt, routeInformer, configurationInformer, ingressInformer,
			cfg, autoscaleEnableScaleToZero),
		service.NewController(opt, serviceInformer, configurationInformer, routeInformer, cfg),
	}

	// These are non-blocking.
	kubeInformerFactory.Start(stopCh)
	elaInformerFactory.Start(stopCh)
	buildInformerFactory.Start(stopCh)
	servingSystemInformerFactory.Start(stopCh)
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
		buildInformer.Informer().HasSynced,
		deploymentInformer.Informer().HasSynced,
		coreServiceInformer.Informer().HasSynced,
		endpointsInformer.Informer().HasSynced,
		ingressInformer.Informer().HasSynced,
	} {
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			logger.Fatalf("failed to wait for cache at index %v to sync", i)
		}
	}

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

	<-stopCh
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&queueSidecarImage, "queueSidecarImage", "", "The digest of the queue sidecar image.")
	flag.StringVar(&autoscalerImage, "autoscalerImage", "", "The digest of the autoscaler image.")
}
