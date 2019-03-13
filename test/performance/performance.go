/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package performance

import (
	"fmt"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/zipkin"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/junit"
	"github.com/knative/test-infra/shared/prometheus"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	istioNS      = "istio-system"
	monitoringNS = "knative-monitoring"
	gateway      = "istio-ingressgateway"
	// Property name used by testgrid.
	perfLatency = "perf_latency"
	duration    = 1 * time.Minute
)

type RequirePrometheus bool

const (
	EnablePrometheus  RequirePrometheus = true
	DisablePrometheus                   = false
)

type RequireZipkin bool

const (
	EnableZipkin  RequireZipkin = true
	DisableZipkin               = false
)

// Client is the client used in the performance tests.
type Client struct {
	E2EClients *test.Clients
	PromClient *prometheus.PromProxy
}

// Setup creates all the clients that we need to interact with in our tests
func Setup(logf logging.FormatLogger, promReqd RequirePrometheus, zipkinReq RequireZipkin) (*Client, error) {
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		return nil, err
	}

	var p *prometheus.PromProxy
	if promReqd {
		logf("Creating prometheus proxy client")
		p = &prometheus.PromProxy{Namespace: monitoringNS}
		p.Setup(clients.KubeClient.Kube, logf)
	}

	if zipkinReq {
		zipkin.SetupZipkinTracing(clients.KubeClient.Kube, logf)
	}

	return &Client{E2EClients: clients, PromClient: p}, nil
}

// TearDown cleans up resources used
func TearDown(client *Client, names test.ResourceNames, logf logging.FormatLogger) {
	test.TearDown(client.E2EClients, names)

	if client.PromClient != nil {
		client.PromClient.Teardown(logf)
	}

	zipkin.CleanupZipkinTracingSetup(logf)
}

// CreatePerfTestCase creates a perf test case with the provided name and value
func CreatePerfTestCase(metricValue float32, metricName, testName string) junit.TestCase {
	tp := []junit.TestProperty{{Name: perfLatency, Value: fmt.Sprintf("%f", metricValue)}}
	tc := junit.TestCase{
		ClassName:  testName,
		Name:       fmt.Sprintf("%s/%s", testName, metricName),
		Properties: junit.TestProperties{Properties: tp}}
	return tc
}
