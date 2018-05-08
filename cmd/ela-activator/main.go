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
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/elafros/elafros/pkg/activator"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	"github.com/elafros/elafros/pkg/controller"
	"github.com/elafros/elafros/pkg/signals"
	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	deduper *activator.Deduper
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

	a := activator.NewRevisionActivator(kubeClient, elaClient)
	deduper = activator.NewDeduppingActivator(a)

	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}

func handler(w http.ResponseWriter, r *http.Request) {
	// TODO: Use the namespace from the header.
	// https://github.com/elafros/elafros/issues/693
	id := RevisionId{
		namespace: "default",
		name:      r.Header.Get(controller.GetRevisionHeaderName()),
	}
	endpoint, status, err := deduper.ActiveEndpoint(id)
	if err != nil {
		msg := fmt.Sprintf("Error getting active endpoint: %v", err)
		log.Errorf(msg)
		http.Error(w, msg, status)
		return
	}
	target := &url.URL{
		// TODO: support https
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", endpoint.Ip, endpoint.Port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ServeHTTP(w, r)
}
