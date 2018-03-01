/*
Copyright 2017 Google Inc. All Rights Reserved.
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
	"github.com/elafros/elafros/pkg/signals"
	"github.com/elafros/elafros/pkg/webhook"

	"github.com/golang/glog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	flag.Parse()
	glog.Info("Starting the Configuration Webhook...")

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		glog.Fatal(err)
	}

	options := webhook.ControllerOptions{
		ServiceName:      "ela-webhook",
		ServiceNamespace: "ela-system",
		Port:             443,
		SecretName:       "ela-webhook-certs",
		WebhookName:      "webhook.elafros.dev",
	}
	controller, err := webhook.NewAdmissionController(clientset, options)
	if err != nil {
		glog.Fatal(err)
	}
	controller.Run(stopCh)
}
