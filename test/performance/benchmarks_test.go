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
	"net/http"
	"strings"
	"testing"
	"time"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
	"knative.dev/test-infra/shared/junit"
	perf "knative.dev/test-infra/shared/performance"
	"knative.dev/test-infra/shared/testgrid"

	vegeta "github.com/tsenart/vegeta/lib"
	"knative.dev/pkg/test/vegeta/pacers"
)

const (
	reqTimeout = 30 * time.Second
	app        = "autoscale"
)

var loads = [...]int{100, 1000}

func filename(name string) string {
	// Replace characters in `name` with characters for a file name.
	return strings.ReplaceAll(name, "/", "-")
}

func runTest(t *testing.T, pacer vegeta.Pacer, saveMetrics bool) {
	t.Helper()
	tName := t.Name()
	perfClients, err := Setup(t)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}

	clients := perfClients.E2EClients
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   app,
	}

	defer TearDown(perfClients, names, t.Logf)
	test.CleanupOnInterrupt(func() { TearDown(perfClients, names, t.Logf) })

	t.Log("Creating a new Service")
	objs, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	domain := objs.Route.Status.URL.Host
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing domain %s: %v", domain, err)
	}

	endpoint, err := spoof.ResolveEndpoint(clients.KubeClient.Kube, domain, test.ServingFlags.ResolvableDomain,
		pkgTest.Flags.IngressEndpoint)
	if err != nil {
		t.Fatalf("Cannot resolve service endpoint: %v", err)
	}

	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: http.MethodGet,
		Header: resolvedHeaders(hostname, test.ServingFlags.ResolvableDomain),
		URL:    sanitizedURL(endpoint) + "?sleep=100",
	})
	attacker := vegeta.NewAttacker()

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, pacer, duration, tName) {
		metrics.Add(res)
	}
	metrics.Close()

	// Return directly if we do not want to save metrics for this test run.
	if !saveMetrics {
		return
	}

	var tc []junit.TestCase
	// Add latency metrics
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.P50.Seconds()*1000), "p50(ms)", tName))
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.Quantile(0.90).Seconds()*1000), "p90(ms)", tName))
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.P99.Seconds()*1000), "p99(ms)", tName))

	// Add errorsPercentage metrics
	tc = append(tc, perf.CreatePerfTestCase(float32(1-metrics.Success)*100, "errorsPercentage", tName))

	if err = testgrid.CreateXMLOutput(tc, filename(tName)); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}

// TestBenchmarkSteadyTraffic generates steady traffic at different volumes.
func TestBenchmarkSteadyTraffic(t *testing.T) {
	for _, load := range loads {
		t.Run(fmt.Sprintf("N%d", load), func(t *testing.T) {
			zeroToNSteadyPacer, err := pacers.NewSteadyUp(
				vegeta.Rate{Freq: 1, Per: time.Second},
				vegeta.Rate{Freq: load, Per: time.Second},
				duration/2,
			)
			if err != nil {
				t.Fatalf("Cannot create the SteadyUpPacer: %v", err)
			}
			runTest(t, zeroToNSteadyPacer, true)
		})
	}
}

// TestBenchmarkBurstZeroToN generates a burst from 0 to N concurrent requests, for different values of N.
func TestBenchmarkBurstZeroToN(t *testing.T) {
	for _, load := range loads {
		t.Run(fmt.Sprintf("N%d", load), func(t *testing.T) {
			zeroToNBurstPacer := vegeta.ConstantPacer{Freq: load, Per: time.Second}
			runTest(t, zeroToNBurstPacer, true)
		})
	}
}

// TestBenchmarkBurstNto2N generates a burst from N to 2N concurrent requests, for different values of N.
func TestBenchmarkBurstNto2N(t *testing.T) {
	for _, load := range loads {
		t.Run(fmt.Sprintf("N%d", load), func(t *testing.T) {
			// Steady ramp up from 0 to N, then burst to 2N.
			zeroToNSteadyPacer, err := pacers.NewSteadyUp(
				vegeta.Rate{Freq: 1, Per: time.Second},
				vegeta.Rate{Freq: load, Per: time.Second},
				duration,
			)
			if err != nil {
				t.Fatalf("Cannot create the SteadyUpPacer: %v", err)
			}
			// Scale up to the desired load and discard the metrics.
			runTest(t, zeroToNSteadyPacer, false)

			nTo2NBurstPacer := vegeta.ConstantPacer{Freq: 2 * load, Per: time.Second}
			runTest(t, nTo2NBurstPacer, true)
		})
	}
}
