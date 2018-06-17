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
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/golang/glog"
	"github.com/knative/serving/pkg/activator"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/signals"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	maxRetry = 10
)

type activationHandler struct {
	act activator.Activator
}

// retryRoundTripper retries on 503's for up to 60 seconds. The reason is there is
// a small delay for k8s to include the ready IP in service.
// https://github.com/knative/serving/issues/660#issuecomment-384062553
type retryRoundTripper struct{}

func (rrt retryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	transport := http.DefaultTransport.(*http.Transport)
	timeout := 500 * time.Millisecond

	i := 0
	for ; i < maxRetry; i++ {
		transport.DialContext = (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
		resp, err := transport.RoundTrip(r)
		if err == nil && resp != nil {
			//defer resp.Body.Close()
			if resp.StatusCode != 503 {
				return resp, nil
			}
		}
		timeout = timeout + timeout
		log.Printf("Retrying request to %v in %v", r.URL.Host, timeout)
		time.Sleep(timeout)
	}
	// TODO: add metrics for number of tries and the response code.
	return transport.RoundTrip(r)
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
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", endpoint.FQDN, endpoint.Port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = retryRoundTripper{}
	// TODO: Clear the host to avoid 404's.
	// https://github.com/elafros/elafros/issues/964
	r.Host = ""
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
