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

// latency_test.go brings up a helloworld app and gets the latency metric

package performance

import (
	"fmt"
	"net/http"
	"net/url"
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
)

const (
	sleepTime = 1 * time.Minute
	// sleepReqTimeout should be > sleepTime. Else, the request will time out before receiving the response
	sleepReqTimeout = 2 * time.Minute
	hwReqtimeout    = 30 * time.Second
	baseQPS         = 10
)

func timeToServe(t *testing.T, img, query string, reqTimeout time.Duration) {
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
	objs, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false /* https TODO(taragu) turn this on after helloworld test running with https */)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	routeURL := objs.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		routeURL,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing %s: %v", routeURL, err)
	}

	endpoint, err := spoof.ResolveEndpoint(clients.KubeClient.Kube, routeURL.Hostname(), test.ServingFlags.ResolvableDomain,
		pkgTest.Flags.IngressEndpoint)
	if err != nil {
		t.Fatalf("Cannot resolve service endpoint: %v", err)
	}

	u, _ := url.Parse(routeURL.String())
	u.Host = endpoint
	pacer := vegeta.ConstantPacer{Freq: baseQPS, Per: time.Second}
	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: http.MethodGet,
		Header: resolvedHeaders(routeURL.Hostname(), test.ServingFlags.ResolvableDomain),
		URL:    u.String() + "?" + query,
	})
	attacker := vegeta.NewAttacker()

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, pacer, duration, tName) {
		metrics.Add(res)
	}
	metrics.Close()

	var tc []junit.TestCase
	// Add latency metrics
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.P50.Seconds()*1000), "p50(ms)", tName))
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.Quantile(0.90).Seconds()*1000), "p90(ms)", tName))
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.P99.Seconds()*1000), "p99(ms)", tName))

	if err = testgrid.CreateXMLOutput(tc, tName); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}

// Performs perf test on the hello world app
func TestTimeToServeLatency(t *testing.T) {
	timeToServe(t, "helloworld", "", hwReqtimeout)
}

// Performs perf testing on a long running app.
// It uses the timeout app that sleeps for the specified amount of time.
func TestTimeToServeLatencyLongRunning(t *testing.T) {
	q := fmt.Sprintf("timeout=%d", sleepTime.Milliseconds())
	timeToServe(t, "timeout", q, sleepReqTimeout)
}
