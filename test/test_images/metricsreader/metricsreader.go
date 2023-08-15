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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/serving/pkg/metrics"
	pkgnet "knative.dev/serving/pkg/networking"
	"knative.dev/serving/test"
	"knative.dev/serving/test/test_images/metricsreader/helpers"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const (
	ActivatorMetricKey = "activator_request_count"
	QueueMetricKey     = "revision_request_count"
)

type MetricsClient struct {
	http.Client
}

func (mc *MetricsClient) GetMetricsData(source string) (map[string]*io_prometheus_client.MetricFamily, error) {
	resp, err := mc.Get(fmt.Sprintf("http://%s/metrics", source))
	if err != nil {
		return nil, fmt.Errorf("error: couldn't call %s metrics endpoint: %w", source, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error: couldn't read response body from %s metrics endpoint: %w", source, err)
	}
	return parseMetricsData(body)
}

func NewMetricsClient(d *helpers.PostData) *MetricsClient {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		Dial:                dialer.Dial,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "activator:80" {
				addr = fmt.Sprintf("%s:9090", d.ActivatorIP)
			} else if addr == "queue:80" {
				addr = fmt.Sprintf("%s:%d", d.QueueIP, pkgnet.UserQueueMetricsPort)
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}

	return &MetricsClient{http.Client{
		Transport: &transport,
		Timeout:   4 * time.Second,
	}}
}

func buildResponse(activatorRequestCount, queueRequestCount *io_prometheus_client.MetricFamily) *helpers.ResponseData {
	results := helpers.NewResponse()

	for _, l := range activatorRequestCount.Metric[0].Label {
		if *l.Name == string(metrics.LabelSecurityMode) {
			results.Activator[netcfg.Trust(*l.Value)] = int(*activatorRequestCount.Metric[0].Counter.Value)
		}
	}

	for _, l := range queueRequestCount.Metric[0].Label {
		if *l.Name == string(metrics.LabelSecurityMode) {
			results.Queue[netcfg.Trust(*l.Value)] = int(*queueRequestCount.Metric[0].Counter.Value)
		}
	}

	return results
}

func parseMetricsData(d []byte) (map[string]*io_prometheus_client.MetricFamily, error) {
	//parse the response
	var parser expfmt.TextParser
	mf, err := parser.TextToMetricFamilies(bytes.NewReader(d))
	if err != nil {
		return nil, fmt.Errorf("error: couldn't parse metrics output: %w", err)
	}
	return mf, nil
}

func getMetrics(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "404 not found.", http.StatusNotFound)
	}
	switch r.Method {
	case "GET":
		log.Print("Metricsreader received a GET request.")
		w.Write([]byte("Come back with a POST for metrics."))
	case "POST":
		log.Print("Metricsreader received a POST request.")

		decoder := json.NewDecoder(r.Body)
		var d helpers.PostData
		err := decoder.Decode(&d)
		if err != nil {
			msg := fmt.Errorf("failed to decode POST data: %w", err)
			log.Print(msg)
			http.Error(w, msg.Error(), http.StatusInternalServerError)
		}

		metricsClient := NewMetricsClient(&d)

		activatorData, err := metricsClient.GetMetricsData("activator")
		if err != nil {
			log.Print(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		queueData, err := metricsClient.GetMetricsData("queue")
		if err != nil {
			log.Print(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		results := buildResponse(activatorData[ActivatorMetricKey], queueData[QueueMetricKey])

		jsonResponseData, err := json.Marshal(results)
		if err != nil {
			msg := fmt.Errorf("failed to marshal results to json: %w", err)
			log.Print(msg)
			http.Error(w, msg.Error(), http.StatusInternalServerError)
		} else {
			w.Write(jsonResponseData)
		}

	default:
		msg := "Sorry, only GET and POST methods are supported."
		log.Print(msg)
		http.Error(w, msg, http.StatusMethodNotAllowed)
	}
}

func main() {
	log.Print("metricsreader app started.")

	test.ListenAndServeGracefully(":8080", getMetrics)
}
