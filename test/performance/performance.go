// +build performance

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
	"context"
	"fmt"
	"os"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/prometheus"
	"github.com/knative/test-infra/tools/testgrid"
	"istio.io/fortio/fhttp"
	"istio.io/fortio/periodic"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	istioNS      = "istio-system"
	monitoringNS = "knative-monitoring"
	gateway      = "knative-ingressgateway"
)

type PerformanceClient struct {
	E2EClients *test.Clients
	PromClient *prometheus.PromProxy
}

// Setup creates all the clients that we need to interact with in our tests
func Setup(ctx context.Context, logger *logging.BaseLogger, promReqd bool) (*PerformanceClient, error) {
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		return nil, err
	}

	var p *prometheus.PromProxy
	if promReqd {
		logger.Infof("Creating prometheus proxy client")
		p = &prometheus.PromProxy{Namespace: monitoringNS}
		p.Setup(ctx, logger)
	}
	return &PerformanceClient{E2EClients: clients, PromClient: p}, nil
}

// Teardown cleans up resources used
func TearDown(client *PerformanceClient, logger *logging.BaseLogger, names test.ResourceNames) {
	if client.E2EClients != nil && client.E2EClients.ServingClient != nil {
		client.E2EClients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}

	if client.PromClient != nil {
		client.PromClient.Teardown(logger)
	}
}

// Get the aritfacts directory where we should put the artifacts
func getArtifactsDir() string {
	dir := os.Getenv("ARTIFACTS")
	if dir == "" {
		return "./artifacts"
	}
	return dir
}

func CreateTestgridXML(tc []testgrid.TestCase) error {
	ts := testgrid.TestSuite{TestCases: tc}
	return testgrid.CreateXMLOutput(ts, getArtifactsDir())
}

// RunLoadTest runs the load test with fortio and returns the reponse
func RunLoadTest(duration time.Duration, nThreads, nConnections int, url, domain string) (*fhttp.HTTPRunnerResults, error) {
	o := fhttp.NewHTTPOptions(url)
	o.NumConnections = nConnections
	o.AddAndValidateExtraHeader(fmt.Sprintf("Host: %s", domain))

	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			Duration:    duration,
			NumThreads:  nThreads,
			Percentiles: []float64{50.0, 90.0, 99.0},
		},
		HTTPOptions:        *o,
		AllowInitialErrors: true,
	}

	return fhttp.RunHTTPTest(&opts)
}
