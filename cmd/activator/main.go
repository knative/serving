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
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/knative/serving/pkg/activator"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/configmap"
	"github.com/knative/serving/pkg/controller"
	h2cutil "github.com/knative/serving/pkg/h2c"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/signals"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	maxUploadBytes = 32e6 // 32MB - same as app engine
	maxRetry       = 60
	retryInterval  = 1 * time.Second
)

type activationHandler struct {
	act      activator.Activator
	logger   *zap.SugaredLogger
	reporter activator.StatsReporter
}

// retryRoundTripper retries on 503's for up to 60 seconds. The reason is there is
// a small delay for k8s to include the ready IP in service.
// https://github.com/knative/serving/issues/660#issuecomment-384062553
type retryRoundTripper struct {
	logger   *zap.SugaredLogger
	reporter activator.StatsReporter
}

func (rrt retryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	var err error
	var reqBody *bytes.Reader

	transport := http.DefaultTransport

	if r.ProtoMajor == 2 {
		transport = h2cutil.NewTransport()
	}

	if r.Body != nil {
		reqBytes, err := ioutil.ReadAll(r.Body)

		if err != nil {
			rrt.logger.Errorf("Error reading request body: %s", err)
			return nil, err
		}

		reqBody = bytes.NewReader(reqBytes)
		r.Body = ioutil.NopCloser(reqBody)
	}

	resp, err := transport.RoundTrip(r)
	// TODO: Activator should retry with backoff.
	// https://github.com/knative/serving/issues/1229
	i := 1
	for ; i < maxRetry; i++ {
		if err == nil && resp != nil && resp.StatusCode != 503 {
			break
		}

		if err != nil {
			rrt.logger.Errorf("Error making a request: %s", err)
		}

		if resp != nil {
			resp.Body.Close()
		}

		time.Sleep(retryInterval)

		// The request body cannot be read multiple times for retries.
		// The workaround is to clone the request body into a byte reader
		// so the body can be read multiple times.
		if r.Body != nil {
			reqBody.Seek(0, io.SeekStart)
		}

		resp, err = transport.RoundTrip(r)
	}

	if resp != nil {
		rrt.logger.Infof("It took %d tries to get response code %d", i, resp.StatusCode)
		namespace := r.Header.Get(controller.GetRevisionHeaderNamespace())
		name := r.Header.Get(controller.GetRevisionHeaderName())
		config := r.Header.Get(controller.GetConfigurationHeader())
		rrt.reporter.ReportResponse(namespace, config, name, resp.StatusCode, i, 1.0)
	}
	return resp, nil
}

func (a *activationHandler) handler(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxUploadBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	namespace := r.Header.Get(controller.GetRevisionHeaderNamespace())
	name := r.Header.Get(controller.GetRevisionHeaderName())
	config := r.Header.Get(controller.GetConfigurationHeader())
	endpoint, status, err := a.act.ActiveEndpoint(namespace, config, name)
	if err != nil {
		msg := fmt.Sprintf("Error getting active endpoint: %v", err)
		http.Error(w, msg, int(status))
		a.logger.Errorf(msg)
		a.reporter.ReportResponse(namespace, config, name, int(status), 1, 1.0)
		return
	}
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", endpoint.FQDN, endpoint.Port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = retryRoundTripper{
		logger:   a.logger,
		reporter: a.reporter,
	}

	// TODO: Clear the host to avoid 404's.
	// https://github.com/knative/serving/issues/964
	r.Host = ""

	proxy.ServeHTTP(w, r)
}

func main() {
	flag.Parse()
	config, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}
	logger := logging.NewLoggerFromConfig(logging.NewConfigFromMap(config), "activator")
	defer logger.Sync()

	logger.Info("Starting the knative activator")

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Error getting in cluster configuration", zap.Error(err))
	}
	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Error building new config", zap.Error(err))
	}
	servingClient, err := clientset.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatal("Error building serving clientset: %v", zap.Error(err))
	}

	logger.Info("Initializing OpenCensus Prometheus exporter.")
	promExporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "activator"})
	if err != nil {
		logger.Fatal("Failed to create the Prometheus exporter: %v", zap.Error(err))
	}
	view.RegisterExporter(promExporter)
	view.SetReportingPeriod(10 * time.Second)

	reporter, err := activator.NewStatsReporter()
	if err != nil {
		logger.Fatal("Failed to create stats reporter: %v", zap.Error(err))
	}

	a := activator.NewRevisionActivator(kubeClient, servingClient, logger, reporter)
	a = activator.NewDedupingActivator(a, servingClient, logger, reporter)
	ah := &activationHandler{a, logger, reporter}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()
	go func() {
		<-stopCh
		a.Shutdown()
	}()

	// Start the endpoint for Prometheus scraping
	mux := http.NewServeMux()
	mux.HandleFunc("/", ah.handler)
	mux.Handle("/metrics", promExporter)
	http.ListenAndServe(":8080", mux)
}
