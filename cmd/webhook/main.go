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

	"go.uber.org/zap"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/pkg/signals"
	"github.com/knative/pkg/webhook"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	net "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/system"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	logLevelKey = "webhook"
)

func main() {
	flag.Parse()
	cm, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}
	config, err := logging.NewConfigFromMap(cm)
	if err != nil {
		log.Fatalf("Error parsing logging configuration: %v", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(config, logLevelKey)
	defer logger.Sync()
	logger = logger.With(zap.String(logkey.ControllerType, "webhook"))

	logger.Info("Starting the Configuration Webhook")

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to get in cluster config", zap.Error(err))
	}

	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Failed to get the client set", zap.Error(err))
	}

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace)
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, logLevelKey))
	if err = configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start configuration manager: %v", err)
	}

	options := webhook.ControllerOptions{
		ServiceName:    "webhook",
		DeploymentName: "webhook",
		Namespace:      system.Namespace,
		Port:           443,
		SecretName:     "webhook-certs",
		WebhookName:    "webhook.serving.knative.dev",
	}
	controller := webhook.AdmissionController{
		Client:  kubeClient,
		Options: options,
		Handlers: map[schema.GroupVersionKind]webhook.GenericCRD{
			v1alpha1.SchemeGroupVersion.WithKind("Revision"):      &v1alpha1.Revision{},
			v1alpha1.SchemeGroupVersion.WithKind("Configuration"): &v1alpha1.Configuration{},
			v1alpha1.SchemeGroupVersion.WithKind("Route"):         &v1alpha1.Route{},
			v1alpha1.SchemeGroupVersion.WithKind("Service"):       &v1alpha1.Service{},
			kpa.SchemeGroupVersion.WithKind("PodAutoscaler"):      &kpa.PodAutoscaler{},
			net.SchemeGroupVersion.WithKind("ClusterIngress"):     &net.ClusterIngress{},
		},
		Logger: logger,
	}
	if err != nil {
		logger.Fatal("Failed to create the admission controller", zap.Error(err))
	}
	controller.Run(stopCh)
}
