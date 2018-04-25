/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"net/http"

	"github.com/elafros/elafros/pkg/activator"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	"github.com/elafros/elafros/pkg/controller"
	"github.com/elafros/elafros/pkg/signals"
	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	incomingRequests chan<- *activator.HttpRequests
)

func main() {
	flag.Parse()
	glog.Info("Starting the elafros activator...")

	// set up signals so we handle the first shutdown signal gracefully
	// TODO: wire shutdown signal into sub-components.
	stopCh := signals.SetupSignalHandler()

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err)
	}
	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		glog.Fatal(err)
	}
	elaClient, err := clientset.NewForConfig(clusterConfig)
	if err != nil {
		glog.Fatalf("Error building ela clientset: %v", err)
	}

	httpRequests := make(chan *activator.HttpRequests)
	activationRequests := make(chan *activator.RevisionId)
	endpoints := make(chan *activator.RevisionEndpoint)
	proxyRequests := make(chan *activator.ProxyRequest)

	// Create the Activator, RevisionActivator and Proxy components
	// and wire them together.
	a := activator.NewActivator(
		httpRequests.(<-chan *activator.HttpRequest),
		activationRequests.(chan<- *activator.RevisionId),
		endpoints.(<-chan *activator.RevisionEndpoint),
		proxyRequests.(chan<- *activator.ProxyRequest))
	r := activator.NewRevisionActivator(
		kubeClient,
		elaClient,
		activationRequests.(<-chan *activator.RevisionId),
		endpoints.(chan<- *activator.RevisionEndpoint))
	p := activator.NewProxy(proxyRequests.(<-chan activator.ProxyRequest))
	incomingRequests = httpRequests.(chan<- *activator.HttpRequest)

	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}

func handler(w http.ResponseWriter, r *http.Request) {
	// TODO: Use the namespace from the header.
	// https://github.com/elafros/elafros/issues/693
	namespace := "default"
	name := r.Header.Get(controller.GetRevisionHeaderName())
	incomingRequests <- activator.NewHttpRequests(w, r, namespace, name)
}
