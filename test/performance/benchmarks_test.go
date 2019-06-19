// +build performance

/*
Copyright 2019 The Knative Authors

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
	"strings"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/ingress"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
	"github.com/knative/test-infra/shared/junit"
	"github.com/knative/test-infra/shared/loadgenerator"
	perf "github.com/knative/test-infra/shared/performance"
	"github.com/knative/test-infra/shared/testgrid"
)

const (
	reqTimeout = 30 * time.Second
	app        = "helloworld"
)

var loads = [...]int32{1, 100, 1000}

func filename(name string) string {
	// Replace characters in `name` with characters for a file name.
	return strings.ReplaceAll(name, "/", "-")
}

func runTest(t *testing.T, img string, baseQPS float64, loadFactors []float64) {
	t.Helper()
	tName := t.Name()
	perfClients, err := Setup(t)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}

	clients := perfClients.E2EClients
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   img,
	}

	defer TearDown(perfClients, names, t.Logf)
	test.CleanupOnInterrupt(func() { TearDown(perfClients, names, t.Logf) })

	t.Log("Creating a new Service")
	objs, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	domain := objs.Route.Status.URL.Host
	endpoint, err := ingress.GetIngressEndpoint(clients.KubeClient.Kube)
	if err != nil {
		t.Fatalf("Cannot get service endpoint: %v", err)
	}

	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing domain %s: %v", domain, err)
	}

	opts := loadgenerator.GeneratorOptions{
		Duration:       duration,
		NumThreads:     1,
		NumConnections: 5,
		Domain:         domain,
		URL:            fmt.Sprintf("http://%s", *endpoint),
		RequestTimeout: reqTimeout,
		BaseQPS:        baseQPS,
		LoadFactors:    loadFactors,
		FileNamePrefix: filename(tName),
	}
	resp, err := opts.RunLoadTest(loadgenerator.AddHostHeader)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}

	// Save the json result for benchmarking
	resp.SaveJSON()

	if len(resp.Result) == 0 {
		t.Fatal("No result found for the load test")
	}

	if resp.ErrorsPercentage(0) > 0 {
		t.Fatal("Found non 200 response")
	}

	// Add latency metrics
	var tc []junit.TestCase
	for _, p := range resp.Result[0].DurationHistogram.Percentiles {
		val := float32(p.Value) * 1000
		name := fmt.Sprintf("p%d(ms)", int(p.Percentile))
		tc = append(tc, perf.CreatePerfTestCase(val, name, tName))
	}

	if err = testgrid.CreateXMLOutput(tc, filename(tName)); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}

// TestBenchmarkSteadyTraffic generates steady traffic at different volumes.
func TestBenchmarkSteadyTraffic(t *testing.T) {
	for _, load := range loads {
		t.Run(fmt.Sprintf("N%d", load), func(t *testing.T) {
			runTest(t, app, float64(load), []float64{1})
		})
	}
}

// TestBenchmarkBurstZeroToN generates a burst from 0 to N concurrent requests, for different values of N.
func TestBenchmarkBurstZeroToN(t *testing.T) {
	for _, load := range loads {
		t.Run(fmt.Sprintf("N%d", load), func(t *testing.T) {
			runTest(t, app, float64(load), []float64{0, 1})
		})
	}
}

// TestBenchmarkBurstNto2N generates a burst from N to 2N concurrent requests, for different values of N.
func TestBenchmarkBurstNto2N(t *testing.T) {
	for _, load := range loads {
		t.Run(fmt.Sprintf("N%d", load), func(t *testing.T) {
			runTest(t, app, float64(load), []float64{1, 2})
		})
	}
}
