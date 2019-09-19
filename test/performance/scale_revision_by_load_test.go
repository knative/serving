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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"knative.dev/test-infra/shared/junit"
	perf "knative.dev/test-infra/shared/performance"
	"knative.dev/test-infra/shared/testgrid"

	vegeta "github.com/tsenart/vegeta/lib"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/controller"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/resources"
	testingv1alpha1 "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

const (
	qpsPerClient         = 10               // frequencies of requests per client
	iterationDuration    = 60 * time.Second // iteration duration for a single scale
	processingTimeMillis = 100              // delay of each request on "server" side
	targetValue          = 10
)

var concurrentClients = []int{10, 20, 40, 80, 160, 320}

type scaleEvent struct {
	oldScale  int
	newScale  int
	timestamp time.Time
}

// TestScaleRevisionByLoad performs several iterations with increasing number of clients
// while measuring response times, error rates, and time to scale up.
func TestScaleRevisionByLoad(t *testing.T) {
	tc := make([]junit.TestCase, 0)
	for _, numClients := range concurrentClients {
		t.Run(fmt.Sprintf("clients-%03d", numClients), func(t *testing.T) {
			tc = append(tc, scaleRevisionByLoad(t, numClients)...)
		})
	}
	if err := testgrid.CreateXMLOutput(tc, t.Name()); err != nil {
		t.Fatalf("Cannot create output XML: %v", err)
	}
}

func scaleRevisionByLoad(t *testing.T, numClients int) []junit.TestCase {
	perfClients, err := Setup(t)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "observed-concurrency",
	}
	clients := perfClients.E2EClients

	defer TearDown(perfClients, names, t.Logf)
	test.CleanupOnInterrupt(func() { TearDown(perfClients, names, t.Logf) })

	t.Log("Creating a new Service")
	objs, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		testingv1alpha1.WithResourceRequirements(corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("20Mi"),
			},
		}),
		testingv1alpha1.WithConfigAnnotations(map[string]string{"autoscaling.knative.dev/target": strconv.Itoa(targetValue)}),
	)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	routeURL := objs.Route.Status.URL.URL()

	// Make sure we are ready to serve.
	u, _ := url.Parse(routeURL.String())
	q := u.Query()
	q.Set("timeout", "10")
	u.RawQuery = q.Encode()
	st := time.Now()
	t.Logf("Starting to probe %s at %s", u, st)
	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		u,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint at %s didn't serve the expected response: %v", u, err)
	}
	t.Logf("Took %v for the endpoint to start serving", time.Since(st))

	// The number of scale events should be at most ~numClients/targetValue
	scaleEvents := make([]*scaleEvent, 0, numClients/targetValue*10)
	var scaleEventsMutex sync.Mutex
	stopCh := make(chan struct{})

	factory := informers.NewSharedInformerFactory(clients.KubeClient.Kube, 0)
	endpointsInformer := factory.Core().V1().Endpoints().Informer()
	endpointsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			newEndpoints := newObj.(*corev1.Endpoints)
			if strings.Contains(newEndpoints.GetName(), names.Service) {
				newNumAddresses := resources.ReadyAddressCount(newEndpoints)
				oldNumAddresses := resources.ReadyAddressCount(oldObj.(*corev1.Endpoints))
				if newNumAddresses != oldNumAddresses {
					event := &scaleEvent{
						oldScale:  oldNumAddresses,
						newScale:  newNumAddresses,
						timestamp: time.Now(),
					}
					scaleEventsMutex.Lock()
					defer scaleEventsMutex.Unlock()
					scaleEvents = append(scaleEvents, event)
				}
			}
		},
	})
	controller.StartInformers(stopCh, endpointsInformer)

	endpoint, err := spoof.ResolveEndpoint(clients.KubeClient.Kube, routeURL.Hostname(), test.ServingFlags.ResolvableDomain,
		pkgTest.Flags.IngressEndpoint)
	if err != nil {
		t.Fatalf("Cannot resolve service endpoint: %v", err)
	}

	u, _ = url.Parse(routeURL.String())
	u.Host = endpoint
	q = u.Query()
	q.Set("timeout", fmt.Sprintf("%d", processingTimeMillis))
	u.RawQuery = q.Encode()
	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: http.MethodGet,
		Header: resolvedHeaders(u.Hostname(), test.ServingFlags.ResolvableDomain),
		URL:    u.String(),
	})
	attacker := vegeta.NewAttacker(vegeta.Workers(uint64(numClients)), vegeta.Connections(numClients))
	pacer := vegeta.ConstantPacer{Freq: qpsPerClient * numClients, Per: time.Second}

	t.Logf("Starting test with %d clients at %s", numClients, time.Now())
	attackStartTime := time.Now()
	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, pacer, iterationDuration, t.Name()) {
		metrics.Add(res)
	}
	metrics.Close()
	close(stopCh)

	tc := make([]junit.TestCase, 0)
	// Add traffic load metrics.
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Requests), "requestCount", t.Name()))
	tc = append(tc, perf.CreatePerfTestCase(float32(qpsPerClient*numClients), "requestedQPS", t.Name()))
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Rate), "actualQPS", t.Name()))
	// Add errorsPercentage metrics.
	tc = append(tc, perf.CreatePerfTestCase(float32(1-metrics.Success)*100, "errorsPercentage", t.Name()))

	scaleEventsMutex.Lock()
	defer scaleEventsMutex.Unlock()
	// Add scale metrics.
	for _, ev := range scaleEvents {
		t.Logf("Scaled: %d -> %d in %v", ev.oldScale, ev.newScale, ev.timestamp.Sub(attackStartTime))
		tc = append(tc, perf.CreatePerfTestCase(float32(ev.timestamp.Sub(attackStartTime)/time.Second), fmt.Sprintf("scale-from-%02d-to-%02d(seconds)", ev.oldScale, ev.newScale), t.Name()))
	}

	// Add latency metrics.
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.P50.Seconds()*1000), "p50(ms)", t.Name()))
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.Quantile(0.90).Seconds()*1000), "p90(ms)", t.Name()))
	tc = append(tc, perf.CreatePerfTestCase(float32(metrics.Latencies.P99.Seconds()*1000), "p99(ms)", t.Name()))

	return tc
}
