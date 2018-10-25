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
	"fmt"
	"os"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/tools/testgrid"
	"istio.io/fortio/fhttp"
	"istio.io/fortio/periodic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	istioNS = "istio-system"
	gateway = "knative-ingressgateway"
)

// Setup creates all the initial setup needed to run the tests
func Setup(t *testing.T) *test.Clients {
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

// Teardown cleans up resources used
func TearDown(clients *test.Clients, names test.ResourceNames) {
	if clients != nil && clients.ServingClient != nil {
		clients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}
}

// GetServiceEndpoint gets the endpoint IP or hostname to use for the service
func GetServiceEndpoint(client *test.Clients) (*string, error) {
	var endpoint string
	ingress, err := client.KubeClient.Kube.CoreV1().Services(istioNS).Get(gateway, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	ingresses := ingress.Status.LoadBalancer.Ingress
	if len(ingresses) != 1 {
		return nil, fmt.Errorf("Expected exactly one ingress load balancer, instead had %d: %s", len(ingresses), ingresses)
	}

	ingressToUse := ingresses[0]
	if ingressToUse.IP == "" {
		if ingressToUse.Hostname == "" {
			return nil, fmt.Errorf("Expected ingress loadbalancer IP or hostname for %s to be set, instead was empty", gateway)
		}
		endpoint = ingressToUse.Hostname
	} else {
		endpoint = ingressToUse.IP
	}
	return &endpoint, nil
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
func RunLoadTest(endpoint, domain string) (*fhttp.HTTPRunnerResults, error) {
	o := fhttp.HTTPOptions{
		URL:            fmt.Sprintf("http://%s/?timeout=1000", endpoint),
		NumConnections: 5,
	}
	o.AddAndValidateExtraHeader(fmt.Sprintf("Host: %s", domain))

	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			Duration:    1 * time.Minute,
			NumThreads:  5,
			Percentiles: []float64{50.0, 90.0, 99.0},
		},
		HTTPOptions: o,
	}

	return fhttp.RunHTTPTest(&opts)
}
