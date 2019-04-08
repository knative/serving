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
	ingress "github.com/knative/pkg/test/ingress"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/junit"
	"github.com/knative/test-infra/shared/loadgenerator"
	"github.com/knative/test-infra/shared/testgrid"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	qpsPerClient         = 10               // frequencies of requests per client
	iterationDuration    = 60 * time.Second // iteration duration for a single scale
	processingTimeMillis = 100              // delay of each request on "server" side
	containerConcurrency = 10
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
	tc := make([]junit.TestCase, 5*len(concurrentClients))
	for _, numClients := range concurrentClients {
		t.Run(fmt.Sprintf("clients-%03d", numClients), func(t *testing.T) {
			scaleRevisionByLoad(t, numClients, tc)
		})
	}
	if err := testgrid.CreateXMLOutput(tc, t.Name()); err != nil {
		t.Fatalf("Cannot create output XML: %v", err)
	}
}

func scaleRevisionByLoad(t *testing.T, numClients int, tc []junit.TestCase) {
	perfClients, err := Setup(t.Logf, true)
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
	objs, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{
		ContainerResources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("20Mi"),
			},
		}},
		func(service *v1alpha1.Service) {
			service.Spec.RunLatest.Configuration.RevisionTemplate.Annotations = map[string]string{"autoscaling.knative.dev/target": fmt.Sprintf("%d", containerConcurrency)}
		},
	)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	domain := objs.Route.Status.Domain
	endpoint, err := ingress.GetIngressEndpoint(clients.KubeClient.Kube)
	if err != nil {
		t.Fatalf("Cannot get service endpoint: %v", err)
	}

	// Make sure we are ready to serve.
	st := time.Now()
	t.Log("Starting to probe the endpoint at", st)
	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain+"/?timeout=10", // To generate any kind of a valid response.
		test.RetryingRouteInconsistency(func(resp *spoof.Response) (bool, error) {
			_, _, err := parseResponse(string(resp.Body))
			return err == nil, nil
		}),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint at domain %s didn't serve the expected response: %v", domain, err)
	}
	t.Logf("Took %v for the endpoint to start serving", time.Since(st))

	scaleChannel := make(chan *scaleEvent)
	endMonitoring := make(chan struct{})
	eg := errgroup.Group{}

	eg.Go(func() error {
		t.Logf("Scale Monitoring Started")
		defer close(scaleChannel)
		previousNumEndpoints := 1 //We start with 1 pod running
		for {
			select {
			case <-endMonitoring:
				t.Log("Scale Monitoring Finished")
				return nil
			default:
				currentNumEndpoints, err := test.NumEndpoints(clients, test.ServingNamespace, names.Service)
				if err != nil {
					t.Logf("Unable to get pod count: %v", err.Error())
				}
				if currentNumEndpoints != previousNumEndpoints {
					event := &scaleEvent{
						oldScale:  previousNumEndpoints,
						newScale:  currentNumEndpoints,
						timestamp: time.Now(),
					}
					scaleChannel <- event
					previousNumEndpoints = currentNumEndpoints
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	})

	events := make([]*scaleEvent, 0)
	eg.Go(func() error {
		for scaleEvent := range scaleChannel {
			events = append(events, scaleEvent)
		}
		return nil
	})

	opts := loadgenerator.GeneratorOptions{
		Duration:       iterationDuration,
		NumThreads:     numClients,
		NumConnections: numClients,
		Domain:         domain,
		QPS:            qpsPerClient * float64(numClients),
		URL:            fmt.Sprintf("http://%s/?timeout=%d", *endpoint, processingTimeMillis),
	}

	t.Logf("Starting test with %d clients at %s", numClients, time.Now())
	resp, err := opts.RunLoadTest(false)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}

	endMonitoring <- struct{}{}

	if err := eg.Wait(); err != nil {
		t.Fatalf("Failed to collect Pod counts: %v", err)
	}

	// Save the json result for benchmarking
	resp.SaveJSON(strings.Replace(t.Name(), "/", "_", -1))

	tc = append(tc, CreatePerfTestCase(float32(resp.Result.DurationHistogram.Count), "requestCount", t.Name()))
	tc = append(tc, CreatePerfTestCase(float32(qpsPerClient*numClients), "requestedQPS", t.Name()))
	tc = append(tc, CreatePerfTestCase(float32(resp.Result.ActualQPS), "actualQPS", t.Name()))
	tc = append(tc, CreatePerfTestCase(float32(errorsPercentage(resp)), "errorsPercentage", t.Name()))

	for _, ev := range events {
		t.Logf("Scaled: %d -> %d in %v", ev.oldScale, ev.newScale, ev.timestamp.Sub(resp.Result.StartTime))
		tc = append(tc, CreatePerfTestCase(float32(ev.timestamp.Sub(resp.Result.StartTime)/time.Second), fmt.Sprintf("scale-from-%02d-to-%02d(seconds)", ev.oldScale, ev.newScale), t.Name()))
	}

	for _, p := range resp.Result.DurationHistogram.Percentiles {
		val := float32(p.Value) * 1000
		name := fmt.Sprintf("p%d(ms)", int(p.Percentile))
		tc = append(tc, CreatePerfTestCase(val, name, t.Name()))
	}
}

func errorsPercentage(resp *loadgenerator.GeneratorResults) float64 {
	var successes, errors int64
	for retCode, count := range resp.Result.RetCodes {
		if retCode == 200 {
			successes = successes + count
		} else {
			errors = errors + count
		}
	}
	return float64(errors*100) / float64(errors+successes)
}
