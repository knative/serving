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
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/testgrid"
	"golang.org/x/sync/errgroup"
)

const (
	tName       = "TestObservedConcurrency"
	concurrency = 5
)

// generateTraffic loads the given endpoint with the given concurrency for the given duration.
// All responses are forwarded to a channel, if given.
func generateTraffic(client *spoof.SpoofingClient, url string, concurrency int, duration time.Duration, resChannel chan *spoof.Response) (int32, error) {
	var (
		group    errgroup.Group
		requests int32
	)

	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			done := time.After(duration)
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("error creating http request: %v", err)
			}
			for {
				select {
				case <-done:
					return nil
				default:
					res, _ := client.Do(req)
					if resChannel != nil {
						resChannel <- res
					}
					atomic.AddInt32(&requests, 1)
				}
			}
		})
	}

	err := group.Wait()
	requestsMade := atomic.LoadInt32(&requests)

	if err != nil {
		return requestsMade, fmt.Errorf("error making requests for scale up: %v", err)
	}
	return requestsMade, nil
}

// event represents the start or end of a request
type event struct {
	concurrencyModifier int32
	timestamp           time.Time
}

// parseResponse parses a string of the form TimeInNano,TimeInNano into the respective
// start and end event
func parseResponse(body string) (*event, *event, error) {
	body = strings.TrimSpace(body)
	parts := strings.Split(body, ",")
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, nil, errors.Wrap(err, fmt.Sprintf("failed to parse start duration, body %s", body))
	}

	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, nil, errors.Wrap(err, fmt.Sprintf("failed to parse end duration, body %s", body))
	}

	startEvent := &event{1, time.Unix(0, int64(start))}
	endEvent := &event{-1, time.Unix(0, int64(end))}

	return startEvent, endEvent, nil
}

// timeToScale calculates the time it took to scale to a given scale, starting from a given
// time. Returns an error if that scale was never reached.
func timeToScale(events []*event, start time.Time, desiredScale int32) (time.Duration, error) {
	var currentConcurrency int32
	for _, event := range events {
		currentConcurrency += event.concurrencyModifier
		if currentConcurrency == desiredScale {
			return event.timestamp.Sub(start), nil
		}
	}

	return 0, fmt.Errorf("desired scale of %d was never reached", desiredScale)
}

func TestObservedConcurrency(t *testing.T) {
	// add test case specific name to its own logger
	logger := logging.GetContextLogger(tName)

	perfClients, err := Setup(context.Background(), logger, true)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}

	names := test.ResourceNames{
		Service: test.AppendRandomString("observed-concurrency", logger),
		Image:   "observed-concurrency",
	}
	clients := perfClients.E2EClients

	defer TearDown(perfClients, logger, names)
	test.CleanupOnInterrupt(func() { TearDown(perfClients, logger, names) }, logger)

	logger.Info("Creating a new Service")
	objs, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{ContainerConcurrency: 1})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	domain := objs.Route.Status.Domain
	endpoint, err := spoof.GetServiceEndpoint(clients.KubeClient.Kube)
	if err != nil {
		t.Fatalf("Cannot get service endpoint: %v", err)
	}

	url := fmt.Sprintf("http://%s/?timeout=1000", *endpoint)
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	trafficStart := time.Now()

	responseChannel := make(chan *spoof.Response, 1000)

	logger.Infof("Running %d concurrent requests for %v", concurrency, duration)
	requestsMade, err := generateTraffic(client, url, concurrency, duration, responseChannel)
	if err != nil {
		t.Fatalf("Failed to generate traffic, err: %v", err)
	}

	// Collect all responses, parse their bodies and create the resulting events.
	var events []*event
	for i := int32(0); i < requestsMade; i++ {
		response := <-responseChannel
		body := string(response.Body)
		start, end, err := parseResponse(body)
		if err != nil {
			logger.Errorf("Failed to parse body")
		} else {
			events = append(events, start, end)
		}
	}

	// Sort all events by their timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].timestamp.Before(events[j].timestamp)
	})

	var tc []testgrid.TestCase
	for i := int32(1); i <= concurrency; i++ {
		toConcurrency, err := timeToScale(events, trafficStart, i)
		if err != nil {
			logger.Infof("Never scaled to %d\n", i)
		} else {
			logger.Infof("Took %v to scale to %d\n", toConcurrency, i)
			tc = append(tc, CreatePerfTestCase(float32(toConcurrency/time.Millisecond), fmt.Sprintf("to%d", i), tName))
		}
	}

	if err = testgrid.CreateTestgridXML(tc, tName); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}
