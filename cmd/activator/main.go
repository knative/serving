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
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/knative/serving/pkg/activator"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/signals"
	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type activationHandler struct {
	act activator.Activator
}

func (a *activationHandler) handler(w http.ResponseWriter, r *http.Request) {
	namespace := r.Header.Get(controller.GetRevisionHeaderNamespace())
	name := r.Header.Get(controller.GetRevisionHeaderName())
	endpoint, status, err := a.act.ActiveEndpoint(namespace, name)
	if err != nil {
		msg := fmt.Sprintf("Error getting active endpoint: %v", err)
		glog.Errorf(msg)
		http.Error(w, msg, int(status))
		return
	}
	target := &url.URL{
		// TODO: Wire Activator into Istio mesh to support TLS
		//       (https://github.com/knative/serving/issues/838)
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", endpoint.IP, endpoint.Port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ServeHTTP(w, r)
}

func main() {
	flag.Parse()
	glog.Info("Starting the knative activator...")

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
	a = activator.NewDedupingActivator(a)
	ah := &activationHandler{a}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()
	go func() {
		<-stopCh
		a.Shutdown()
	}()

	http.HandleFunc("/", ah.handler)
	http.ListenAndServe(":8080", nil)
}
