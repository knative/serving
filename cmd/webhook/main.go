/*
Copyright 2017 The Knative Authors
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

	"github.com/knative/serving/pkg/configmap"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/signals"
	"github.com/knative/serving/pkg/system"
	"github.com/knative/serving/pkg/webhook"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	logger, _ := logging.NewLoggerFromConfig(config, "webhook")
	defer logger.Sync()

	logger.Info("Starting the Configuration Webhook")

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to get in cluster config", zap.Error(err))
	}

	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Failed to get the client set", zap.Error(err))
	}

	options := webhook.ControllerOptions{
		ServiceName:      "webhook",
		ServiceNamespace: system.Namespace,
		Port:             443,
		SecretName:       "webhook-certs",
		WebhookName:      "webhook.knative.dev",
	}
	controller, err := webhook.NewAdmissionController(clientset, options, logger)
	if err != nil {
		logger.Fatal("Failed to create the admission controller", zap.Error(err))
	}
	controller.Run(stopCh)
}
