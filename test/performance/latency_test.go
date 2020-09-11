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
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/junit"
	perf "knative.dev/pkg/test/performance"
	"knative.dev/pkg/test/spoof"
	"knative.dev/pkg/test/testgrid"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"

	vegeta "github.com/tsenart/vegeta/lib"
)

const (
	sleepTime = 1 * time.Minute
	// sleepReqTimeout should be > sleepTime. Else, the request will time out before receiving the response
	sleepReqTimeout = 2 * time.Minute
	hwReqtimeout    = 30 * time.Second
	baseQPS         = 10
	duration        = 1 * time.Minute
)

func timeToServe(t *testing.T, img, query string, reqTimeout time.Duration) {
	t.Helper()
	tName := t.Name()
	clients, err := setup()
	if err != nil {
		t.Fatal("Cannot initialize performance clients:", err)
	}

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   img,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")
	objs, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}

	routeURL := objs.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		routeURL,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.Https),
	); err != nil {
		t.Fatalf("Error probing %s: %v", routeURL, err)
	}

	endpoint, err := spoof.ResolveEndpoint(context.Background(), clients.KubeClient.Kube, routeURL.Hostname(), test.ServingFlags.ResolvableDomain,
		pkgTest.Flags.IngressEndpoint)
	if err != nil {
		t.Fatal("Cannot resolve service endpoint:", err)
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

	tc := []junit.TestCase{
		// Add latency metrics
		perf.CreatePerfTestCase(float32(metrics.Latencies.P50.Seconds()*1000), "p50(ms)", tName),
		perf.CreatePerfTestCase(float32(metrics.Latencies.Quantile(0.90).Seconds()*1000), "p90(ms)", tName),
		perf.CreatePerfTestCase(float32(metrics.Latencies.P99.Seconds()*1000), "p99(ms)", tName),
	}

	if err = testgrid.CreateXMLOutput(tc, tName); err != nil {
		t.Fatal("Cannot create output xml:", err)
	}
}

// Performs perf test on the hello world app
func TestTimeToServeLatency(t *testing.T) {
	timeToServe(t, "helloworld", "", hwReqtimeout)
}

// Performs perf testing on a long running app.
// It uses the timeout app that sleeps for the specified amount of time.
func TestTimeToServeLatencyLongRunning(t *testing.T) {
	q := fmt.Sprint("timeout=", sleepTime.Milliseconds())
	timeToServe(t, "timeout", q, sleepReqTimeout)
}
