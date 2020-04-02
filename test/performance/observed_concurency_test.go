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
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"golang.org/x/sync/errgroup"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/junit"
	perf "knative.dev/pkg/test/performance"
	"knative.dev/pkg/test/spoof"
	"knative.dev/pkg/test/testgrid"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// generateTraffic loads the given endpoint with the given concurrency for the given duration.
// All responses are forwarded to a channel, if given.
func generateTraffic(t *testing.T, client *spoof.SpoofingClient, url string, concurrency int, duration time.Duration, resChannel chan *spoof.Response) error {
	var group errgroup.Group
	// Notify the consumer about the end of the data stream.
	defer close(resChannel)

	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			done := time.After(duration)
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("error creating http request: %w", err)
			}
			for {
				select {
				case <-done:
					return nil
				default:
					res, err := client.Do(req)
					if err != nil {
						t.Logf("Error sending request: %v", err)
					}
					resChannel <- res
				}
			}
		})
	}

	if err := group.Wait(); err != nil {
		return fmt.Errorf("error making requests for scale up: %w", err)
	}
	return nil
}

// event represents the start or end of a request
type event struct {
	concurrencyModifier int
	timestamp           time.Time
}

// parseResponse parses a string of the form TimeInNano,TimeInNano into the respective
// start and end event
func parseResponse(body string) (*event, *event, error) {
	body = strings.TrimSpace(body)
	parts := strings.Split(body, ",")

	if len(parts) < 2 {
		return nil, nil, fmt.Errorf("not enough parts in body, got %q", body)
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse start timestamp, body %q: %w", body, err)
	}

	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse end timestamp, body %q: %w", body, err)
	}

	startEvent := &event{1, time.Unix(0, int64(start))}
	endEvent := &event{-1, time.Unix(0, int64(end))}

	return startEvent, endEvent, nil
}

// timeToScale calculates the time it took to scale to a given scale, starting from a given
// time. Returns an error if that scale was never reached.
func timeToScale(events []*event, desiredScale int) (time.Duration, error) {
	var currentConcurrency int
	start := events[0].timestamp
	for _, event := range events {
		currentConcurrency += event.concurrencyModifier
		if currentConcurrency == desiredScale {
			return event.timestamp.Sub(start), nil
		}
	}

	return 0, fmt.Errorf("desired scale of %d was never reached", desiredScale)
}

func TestObservedConcurrency(t *testing.T) {
	var tc []junit.TestCase
	tests := []int{5, 10, 15} //going beyond 15 currently causes "overload" responses
	for _, clients := range tests {
		t.Run(fmt.Sprintf("scale-%02d", clients), func(t *testing.T) {
			tc = append(tc, testConcurrencyN(t, clients)...)
		})
	}
	if err := testgrid.CreateXMLOutput(tc, t.Name()); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}

func testConcurrencyN(t *testing.T, concurrency int) []junit.TestCase {
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
	objs, err := v1test.CreateServiceReady(t, clients, &names,
		v1opts.WithResourceRequirements(corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("20Mi"),
			},
		}),
		v1opts.WithContainerConcurrency(1))
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	baseURL := objs.Route.Status.URL.URL()

	// See https://github.com/knative/serving/issues/5573 for why we need this
	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		baseURL,
		v1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"ObservedConcurrency",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing %s: %v", baseURL, err)
	}

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, baseURL.Hostname(), test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	// This just helps with preallocation.
	const presumedSize = 1000

	eg := errgroup.Group{}
	responseChannel := make(chan *spoof.Response, presumedSize)
	events := make([]*event, 0, presumedSize)
	failedRequests := 0

	t.Logf("Running %d concurrent requests for %v", concurrency, duration)

	url := fmt.Sprintf("http://%s/?timeout=1000", baseURL.Hostname())
	eg.Go(func() error {
		return generateTraffic(t, client, url, concurrency, duration, responseChannel)
	})
	eg.Go(func() error {
		for response := range responseChannel {
			if response == nil {
				failedRequests++
				continue
			}
			start, end, err := parseResponse(string(response.Body))
			if err != nil {
				t.Logf("Failed to parse the body: %v", err)
				failedRequests++
				continue
			}
			events = append(events, start, end)
		}
		// Sort all events by their timestamp.
		sort.Slice(events, func(i, j int) bool {
			return events[i].timestamp.Before(events[j].timestamp)
		})
		return nil
	})

	if err := eg.Wait(); err != nil {
		t.Fatalf("Failed to generate traffic and process responses: %v", err)
	}
	t.Logf("Generated %d requests with %d failed", len(events)+failedRequests, failedRequests)

	var tc []junit.TestCase
	for i := 2; i <= concurrency; i++ {
		toConcurrency, err := timeToScale(events, i)
		if err != nil {
			t.Logf("Never scaled to %d", i)
		} else {
			t.Logf("Took %v to scale to %d", toConcurrency, i)
			tc = append(tc, perf.CreatePerfTestCase(float32(toConcurrency.Milliseconds()), fmt.Sprintf("to%d(ms)", i), t.Name()))
		}
	}
	tc = append(tc, perf.CreatePerfTestCase(float32(failedRequests), "failed requests", t.Name()))

	return tc
}
