// +build e2e

/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"golang.org/x/sync/errgroup"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rnames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestActivatorOverload makes sure that activator can handle the load when scaling from 0.
// We need to add a similar test for the User pod overload once the second part of overload handling is done.
func TestActivatorOverload(t *testing.T) {
	t.Parallel()

	const (
		// The number of concurrent requests to hit the activator with.
		concurrency = 100
		// How long the service will process the request in ms.
		serviceSleep = 300
	)

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Images:  []string{"timeout"},
	}

	test.EnsureTearDown(t, clients, &names)

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
		context.Background(),
		clients.KubeClient,
		t.Logf,
		resources.Route.Status.URL.URL(),
		v1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", resources.Route.Status.URL.URL(), err)
	}

	deploymentName := rnames.Deployment(resources.Revision)
	if err := WaitForScaleToZero(t, deploymentName, clients); err != nil {
		t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}

	domain := resources.Route.Status.URL.Host
	client, err := pkgTest.NewSpoofingClient(context.Background(), clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		t.Fatal("Error creating the Spoofing client:", err)
	}

	url := fmt.Sprintf("http://%s/?timeout=%d", domain, serviceSleep)

	t.Log("Starting to send out the requests")

	eg, egCtx := errgroup.WithContext(context.Background())
	// Send requests async and wait for the responses.
	for i := 0; i < concurrency; i++ {
		eg.Go(func() error {
			// We need to create a new request per HTTP request because
			// the spoofing client mutates them.
			req, err := http.NewRequestWithContext(egCtx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("error creating http request: %w", err)
			}

			res, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("unexpected error sending a request: %w", err)
			}

			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("status = %d, want: %d, response: %s", res.StatusCode, http.StatusOK, res)
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		t.Error(err)
	}
}
