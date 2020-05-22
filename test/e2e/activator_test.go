// +build e2e

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

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rnames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestActivatorOverload makes sure that activator can handle the load when scaling from 0.
// We need to add a similar test for the User pod overload once the second part of overload handling is done.
func TestActivatorOverload(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	const (
		// The number of concurrent requests to hit the activator with.
		concurrency = 100
		// How long the service will process the request in ms.
		serviceSleep = 300
	)

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "timeout",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a service with run latest configuration.")
	// Create a service with concurrency 1 that sleeps for N ms.
	// Limit its maxScale to 10 containers, wait for the service to scale down and hit it with concurrent requests.
	resources, err := v1test.CreateServiceReady(t, clients, &names,
		func(service *v1.Service) {
			service.Spec.Template.Spec.ContainerConcurrency = ptr.Int64(1)
			service.Spec.Template.Annotations = map[string]string{"autoscaling.knative.dev/maxScale": "10"}
		})
	if err != nil {
		t.Fatal("Unable to create resources:", err)
	}

	// Make sure the service responds correctly before scaling to 0.
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		resources.Route.Status.URL.URL(),
		v1test.RetryingRouteInconsistency(pkgtest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
	); err != nil {
		t.Fatalf("Error probing %s: %v", resources.Route.Status.URL.URL(), err)
	}

	deploymentName := rnames.Deployment(resources.Revision)
	if err := WaitForScaleToZero(t, deploymentName, clients); err != nil {
		t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}

	domain := resources.Route.Status.URL.Host
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
	if err != nil {
		t.Fatal("Error creating the Spoofing client:", err)
	}

	url := fmt.Sprintf("http://%s/?timeout=%d", domain, serviceSleep)

	t.Log("Starting to send out the requests")

	var group sync.WaitGroup
	// Send requests async and wait for the responses.
	for i := 0; i < concurrency; i++ {
		group.Add(1)
		go func() {
			defer group.Done()

			// We need to create a new request per HTTP request because
			// the spoofing client mutates them.
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				t.Errorf("error creating http request: %v", err)
			}

			res, err := client.Do(req)
			if err != nil {
				t.Errorf("unexpected error sending a request, %v", err)
				return
			}

			if res.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want: %d, response: %s", res.StatusCode, http.StatusOK, res)
			}
		}()
	}
	group.Wait()
}

func TestActivatorRevisionTimeout(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	const (
		// The number of concurrent requests to hit the activator with.
		concurrency = 1
		// How long the service will process the request in ms.
		serviceSleep = 10000
		// body of timeout response from activator
		wantTimeoutRespBody = "activator request timeout"
	)
	var (
		group       sync.WaitGroup
		resp        []*spoof.Response
		timeoutResp = 0
		successResp = 0
	)
	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "timeout",
	}
	resp = make([]*spoof.Response, 5)

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a service with run latest configuration.")
	// Create a service with concurrency 1 that sleeps for serviceSleep ms. Limit its maxScale to 1 containers and TBC = -1
	resources, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithContainerConcurrency(1),
		rtesting.WithRevisionTimeoutSeconds(15),
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.MaxScaleAnnotationKey:  "1",
		}))
	if err != nil {
		t.Fatal("Unable to create resources:", err)
	}

	domain := resources.Route.Status.URL.Host
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
	if err != nil {
		t.Fatal("Error creating the Spoofing client:", err)
	}

	url := fmt.Sprintf("http://%s/?timeout=%d", domain, serviceSleep)

	t.Log("Starting to send out requests")

	// Send requests async and wait for the responses.
	for i := 0; i < 5; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			// We need to create a new request per HTTP request because
			// the spoofing client mutates them.
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				t.Errorf("error creating http request: %v", err)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Errorf("unexpected error sending a request, %v", err)
				return
			}
			resp[i] = res
		}(i)
	}
	group.Wait()

	for _, res := range resp {
		switch res.StatusCode {
		case http.StatusOK:
			successResp++
		case http.StatusGatewayTimeout:
			if responseString := string(res.Body); !strings.Contains(responseString, wantTimeoutRespBody) {
				t.Errorf("got = %s, want: %s, response: %s", responseString, wantTimeoutRespBody, res)
				return
			}
			timeoutResp++
		default:
			t.Errorf("Unexpected response: %s", res)
		}
	}
	// Among 5 requests only 2 should be handled by user containers. Of those 2 handled requests, 1st req finishes at 10s and 2nd request gets
	// response bytes written before timeout. Rest of them(3 req) should timeout in activator.
	if successResp != 2 || timeoutResp != 3 {
		t.Errorf("want successful:2 timeout:3, got succeess: %d; timeout: %d; responses collection: %v", successResp, timeoutResp, resp)
	}
}
