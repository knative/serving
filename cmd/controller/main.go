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

	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/knative/pkg/signals"
	v1alpha1reconciler "github.com/knative/serving/pkg/reconciler/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/labeler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/routephase"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/service"
)

const (
	threadsPerController = 2
	logLevelKey          = "controller"
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
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, logLevelKey)
	defer logger.Sync()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatalf("Error building kubeconfig: %v", err)
	}

	resync := 10 * time.Hour // Based on controller-runtime default.
	trackerLease := 3 * resync

	deps, err := v1alpha1reconciler.NewDependencyFactory(cfg, resync)

	if err != nil {
		logger.Fatalf("Error initializing: %v", err)
	}

	configMapWatcher := configmap.NewInformedWatcher(deps.Kubernetes.Client, system.Namespace)

	opt := reconciler.Options{
		KubeClientSet:    deps.Kubernetes.Client,
		SharedClientSet:  deps.Shared.Client,
		ServingClientSet: deps.Serving.Client,
		CachingClientSet: deps.Caching.Client,
		DynamicClientSet: deps.Dynamic.Client,
		ConfigMapWatcher: configMapWatcher,
		Logger:           logger,
		ResyncPeriod:     resync,
		StopChannel:      stopCh,
	}

	buildInformerFactory := revision.KResourceTypedInformerFactory(opt)

	serviceInformer := deps.Serving.InformerFactory.Serving().V1alpha1().Services()
	routeInformer := deps.Serving.InformerFactory.Serving().V1alpha1().Routes()
	configurationInformer := deps.Serving.InformerFactory.Serving().V1alpha1().Configurations()
	revisionInformer := deps.Serving.InformerFactory.Serving().V1alpha1().Revisions()
	kpaInformer := deps.Serving.InformerFactory.Autoscaling().V1alpha1().PodAutoscalers()

	deploymentInformer := deps.Kubernetes.InformerFactory.Apps().V1().Deployments()
	coreServiceInformer := deps.Kubernetes.InformerFactory.Core().V1().Services()
	endpointsInformer := deps.Kubernetes.InformerFactory.Core().V1().Endpoints()
	configMapInformer := deps.Kubernetes.InformerFactory.Core().V1().ConfigMaps()
	imageInformer := deps.Caching.InformerFactory.Caching().V1alpha1().Images()

	routeController, err := v1alpha1reconciler.NewController(
		logger,
		routephase.New,
		"route-controller",
		"Routes",
		configMapWatcher,
		deps,
		trackerLease,
	)

	if err != nil {
		logger.Fatalf("unable to initialize route controller - %v", err)
	}

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
			revisionInformer,
			kpaInformer,
			imageInformer,
			deploymentInformer,
			coreServiceInformer,
			endpointsInformer,
			configMapInformer,
			buildInformerFactory,
		),
		labeler.NewRouteToConfigurationController(
			opt,
			routeInformer,
			configurationInformer,
			revisionInformer,
		),
		routeController,
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
	deps.StartInformers(stopCh)

	if err := configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start configuration manager: %v", err)
	}

	// Wait for the caches to be synced before starting controllers.
	logger.Info("Waiting for informer caches to sync")
	if err := deps.WaitForInformerCacheSync(stopCh); err != nil {
		logger.Fatalf(err.Error())
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
