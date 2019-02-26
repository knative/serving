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
	"fmt"
	"testing"
	"time"
	"net/http"
	"golang.org/x/sync/errgroup"

	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	pkgTest "github.com/knative/pkg/test"
)

// TestActivatorOverload makes sure that activator can handle the load when scaling from 0.
// We need to add a similar test for the User pod overload once the second part of overload handling is done.
func TestActivatorOverload(t *testing.T) {
	t.Parallel()
	const (
		// The number of concurrent requests to hit the activator with.
		// 1000 = the number concurrent connections in Istio.
		concurrency = 1000
		// Timeout to wait for the responses.
		// Ideally we must wait ~30 seconds, TODO: need to figure out where the delta comes from.
		timeout = 65 * time.Second
		// How long the service will process the request in ms.
		serviceSleep = 300
	)
	var (
		clients    = Setup(t)
		group      errgroup.Group
		resChannel = make(chan *spoof.Response, concurrency)
	)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "observed-concurrency",
	}

	fopt := func(service *v1alpha1.Service) {
		service.Spec.RunLatest.Configuration.RevisionTemplate.Spec.ContainerConcurrency = 1
		service.Spec.RunLatest.Configuration.RevisionTemplate.Annotations = map[string]string{"autoscaling.knative.dev/maxScale": "10"}
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a service with run latest configuration.")
	// Create a service with concurrency 1 that could sleep for N ms.
	// Limit its maxScale to 10 containers, wait for the service to scale down and hit it with concurrent requests.
	resources, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{}, fopt)
	if err != nil {
		t.Fatalf("Unable to create resources: %v", err)
	}
	domain := resources.Route.Status.Domain

	deploymentName := resources.Revision.Name + "-deployment"

	t.Log("Waiting for deployment to scale to zero.")
	if err := pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		test.DeploymentScaledToZeroFunc,
		"DeploymentScaledToZero",
		test.ServingNamespace,
		3*time.Minute); err != nil {
		t.Fatalf("Failed waiting for deployment to scale to zero: %v", err)
	}

	url := fmt.Sprintf("http://%s/?timeout=%d", domain, serviceSleep)

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain)
	client.RequestTimeout = timeout

	t.Log("Starting to send out the requests")

	// Send requests async and wait for the responses.
	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("error creating http request: %v", err)
			}
			res, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("unexpected error sending a request, %v", err)
			}
			resChannel <- res
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		t.Fatalf("unexpected error making requests against activator: %v", err)
	}
	t.Log("Done sending out requests")
	close(resChannel)

	t.Log("Process the responses")

	for resp := range resChannel {
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Response code = %d, want: %d", resp.StatusCode, http.StatusOK)
		}
	}
}
