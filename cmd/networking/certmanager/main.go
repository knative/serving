/*
Copyright 2019 The Knative Authors

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

	"go.uber.org/zap"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/clientcmd"

	certmanagerclientset "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/signals"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/certificate"
)

const (
	threadsPerController = 2
	component            = "controller-certificate-cert-manager"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func main() {
	flag.Parse()

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
	defer logger.Sync()

	// Set up signals so we handle the first shutdown signal gracefully.
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatalw("Error building kubeconfig", zap.Error(err))
	}

	opt := reconciler.NewOptionsOrDie(cfg, logger, stopCh)
	certManagerClient, err := certmanagerclientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building cert manager clientset: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(opt.KubeClientSet, opt.ResyncPeriod)
	servingInformerFactory := informers.NewSharedInformerFactory(opt.ServingClientSet, opt.ResyncPeriod)
	cmCertInformerFactory := certmanagerinformers.NewSharedInformerFactory(certManagerClient, opt.ResyncPeriod)

	knCertInformer := servingInformerFactory.Networking().V1alpha1().Certificates()
	cmCertInformer := cmCertInformerFactory.Certmanager().V1alpha1().Certificates()
	configMapInformer := kubeInformerFactory.Core().V1().ConfigMaps()

	// Build our controller
	certificateController := certificate.NewController(
		opt,
		knCertInformer,
		cmCertInformer,
		certManagerClient,
	)

	// Watch the logging config map and dynamically update logging levels.
	opt.ConfigMapWatcher.Watch(logging.ConfigMapName(), logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	if err := opt.ConfigMapWatcher.Start(stopCh); err != nil {
		logger.Fatalw("failed to start configuration manager", zap.Error(err))
	}

	// Wait for the caches to be synced before starting controllers.
	logger.Info("Waiting for informer caches to sync")
	if err := controller.StartInformers(
		stopCh,
		knCertInformer.Informer(),
		cmCertInformer.Informer(),
		configMapInformer.Informer(),
	); err != nil {
		logger.Fatalw("Failed to start informers", zap.Error(err))
	}

	// Start all of the controllers.
	logger.Info("Starting controllers.")
	go controller.StartAll(stopCh, certificateController)
	<-stopCh
}
